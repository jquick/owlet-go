// Decode the raw camera streams the same way the Android app does: H.264 via
// WebCodecs VideoDecoder to a canvas, and AAC via WebCodecs AudioDecoder to a
// WebAudio worklet -> GainNode (volume). No re-encoding, no muxing.

const canvas = document.getElementById('view');
const ctx = canvas.getContext('2d');
const statusEl = document.getElementById('status');
const dot = document.getElementById('dot');
const muteBtn = document.getElementById('mute');
const volSlider = document.getElementById('vol');
const fsBtn = document.getElementById('fs');
const player = document.getElementById('player');

const ICON = {
  on: '<svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor"><path d="M3 9v6h4l5 5V4L7 9H3z"/><path d="M15.5 8.5a5 5 0 0 1 0 7" fill="none" stroke="currentColor" stroke-width="2"/></svg>',
  off: '<svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor"><path d="M3 9v6h4l5 5V4L7 9H3z"/><path d="M16 9l6 6M22 9l-6 6" stroke="currentColor" stroke-width="2" fill="none"/></svg>',
  fs: '<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5"/></svg>',
};

let ws, decoder, configured = false, codec = null;
let lastWallMs = 0;         // server timestamp of the most recent frame
let audio = null;           // {ctx, node, gain, decoder, resample}
let volume = 1;             // 0..1
let muted = true;           // start muted (browsers block audio until a gesture)
let frames = 0, lastFrames = 0, lastT = performance.now();

const setStatus = (t) => { statusEl.textContent = t; };
const hex2 = (n) => n.toString(16).padStart(2, '0');

fsBtn.innerHTML = ICON.fs;
updateMuteIcon();

// Paint the server's timestamp into the video pixels (top-left), so it shows in
// screenshots/recordings and reflects the box clock, not the client's.
function drawClock() {
  if (!lastWallMs) return;
  const d = new Date(lastWallMs);
  const p = (n) => String(n).padStart(2, '0');
  const label = `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ` +
    `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
  const size = Math.max(12, Math.round(canvas.height * 0.032));
  ctx.font = `600 ${size}px ui-monospace, Menlo, Consolas, monospace`;
  ctx.textBaseline = 'alphabetic';
  const padX = Math.round(size * 0.5);
  const padY = Math.round(size * 0.32);
  const margin = Math.round(size * 0.4);
  const m = ctx.measureText(label);
  const ascent = m.actualBoundingBoxAscent;   // top of the glyphs above baseline
  const descent = m.actualBoundingBoxDescent; // ~0 for digits (no descenders)
  const boxH = ascent + descent + padY * 2;
  ctx.fillStyle = 'rgba(0,0,0,0.45)';
  ctx.fillRect(margin, margin, m.width + padX * 2, boxH);
  ctx.fillStyle = '#fff';
  ctx.fillText(label, margin + padX, margin + padY + ascent);
}

function updateMuteIcon() {
  muteBtn.innerHTML = (muted || volume === 0) ? ICON.off : ICON.on;
}

// ---- video --------------------------------------------------------------- //
function codecFromSPS(u8) {
  for (let i = 0; i + 4 < u8.length; i++) {
    if (u8[i] === 0 && u8[i + 1] === 0) {
      let p = -1;
      if (u8[i + 2] === 1) p = i + 3;
      else if (u8[i + 2] === 0 && u8[i + 3] === 1) p = i + 4;
      if (p >= 0 && (u8[p] & 0x1f) === 7 && p + 3 < u8.length)
        return 'avc1.' + hex2(u8[p + 1]) + hex2(u8[p + 2]) + hex2(u8[p + 3]);
    }
  }
  return null;
}

function onFrame(frame) {
  if (canvas.width !== frame.displayWidth || canvas.height !== frame.displayHeight) {
    canvas.width = frame.displayWidth;
    canvas.height = frame.displayHeight;
  }
  ctx.drawImage(frame, 0, 0, canvas.width, canvas.height);
  frame.close();
  drawClock();
  frames++;
  const now = performance.now();
  if (now - lastT >= 1000) {
    const fps = (frames - lastFrames) * 1000 / (now - lastT);
    dot.className = 'dot on';
    setStatus(`${canvas.width}×${canvas.height} · ${fps.toFixed(0)} fps`);
    lastFrames = frames;
    lastT = now;
  }
}

function handleVideo(key, ts, data) {
  if (!configured) {
    if (!codec) codec = codecFromSPS(data);
    if (!codec || !key) return; // wait for a keyframe carrying the SPS
    decoder.configure({ codec, optimizeForLatency: true });
    configured = true;
  }
  if (decoder.decodeQueueSize > 8) return; // shed load if we fall behind
  try {
    decoder.decode(new EncodedVideoChunk({ type: key ? 'key' : 'delta', timestamp: ts, data }));
  } catch (e) { console.error(e); }
}

// ---- audio --------------------------------------------------------------- //
function makeResampler(inRate, outRate) {
  const step = inRate / outRate;
  let pos = 0, prev = 0, primed = false;
  return (input) => {
    const out = [];
    for (let i = 0; i < input.length; i++) {
      const cur = input[i];
      if (!primed) { prev = cur; primed = true; }
      while (pos < 1) { out.push(prev + (cur - prev) * pos); pos += step; }
      pos -= 1;
      prev = cur;
    }
    return Float32Array.from(out);
  };
}

// Lazily build the audio graph; must be called from a user gesture.
async function ensureAudio() {
  if (audio) return;
  const rate = 8000;
  const context = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: rate });
  await context.resume();
  await context.audioWorklet.addModule('worklet.js');
  const node = new AudioWorkletNode(context, 'ring-player');
  const gain = context.createGain();
  node.connect(gain);
  gain.connect(context.destination);
  const resample = makeResampler(rate, context.sampleRate);
  const dec = new AudioDecoder({
    output: (data) => {
      const pcm = new Float32Array(data.numberOfFrames);
      data.copyTo(pcm, { planeIndex: 0, format: 'f32-planar' });
      data.close();
      node.port.postMessage(resample(pcm));
    },
    error: (e) => console.error(e),
  });
  dec.configure({ codec: 'mp4a.40.2', sampleRate: rate, numberOfChannels: 1 });
  audio = { context, node, gain, decoder: dec, resample };
  applyGain();
}

function applyGain() {
  if (audio) audio.gain.gain.value = muted ? 0 : volume;
}

function handleAudio(ts, data) {
  if (!audio) return;
  if (audio.decoder.decodeQueueSize > 16) return;
  try {
    audio.decoder.decode(new EncodedAudioChunk({ type: 'key', timestamp: ts, data }));
  } catch (e) { console.error(e); }
}

// ---- transport ----------------------------------------------------------- //
function onMessage(ev) {
  const dv = new DataView(ev.data);
  const kind = dv.getUint8(0);
  const key = dv.getUint8(1) === 1;
  const ts = Number(dv.getBigUint64(2));
  const wall = Number(dv.getBigUint64(10)); // server wall-clock (ms)
  const payload = new Uint8Array(ev.data, 18);
  if (kind === 0) { lastWallMs = wall; handleVideo(key, ts, payload); }
  else handleAudio(ts, payload);
}

function connect() {
  try { if (decoder && decoder.state !== 'closed') decoder.close(); } catch (e) {}
  configured = false;
  codec = null;
  decoder = new VideoDecoder({ output: onFrame, error: (e) => console.error(e) });

  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const base = location.pathname.replace(/[^/]*$/, ''); // works behind a subpath proxy
  ws = new WebSocket(`${proto}://${location.host}${base}`);
  ws.binaryType = 'arraybuffer';
  ws.onopen = () => { dot.className = 'dot'; setStatus('waiting for video…'); };
  ws.onmessage = onMessage;
  ws.onerror = () => ws.close();
  ws.onclose = () => { dot.className = 'dot'; setStatus('reconnecting…'); setTimeout(connect, 1500); };
}

// ---- controls ------------------------------------------------------------ //
muteBtn.addEventListener('click', async () => {
  await ensureAudio();
  muted = !muted;
  if (!muted && volume === 0) { volume = 1; volSlider.value = 100; }
  applyGain();
  updateMuteIcon();
});

volSlider.addEventListener('input', async () => {
  await ensureAudio();
  volume = volSlider.value / 100;
  muted = volume === 0;
  applyGain();
  updateMuteIcon();
});

fsBtn.addEventListener('click', () => {
  if (document.fullscreenElement) document.exitFullscreen();
  else player.requestFullscreen?.();
});

if (!('VideoDecoder' in window)) {
  setStatus('This browser lacks WebCodecs — use Chrome/Edge.');
} else {
  connect();
}
