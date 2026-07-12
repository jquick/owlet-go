// A ring buffer that plays whatever Float32 PCM the main thread feeds it (the
// web analog of the app's AudioTrack). Silence on underflow, drop-oldest on
// overflow to keep latency bounded.
class RingPlayer extends AudioWorkletProcessor {
  constructor() {
    super();
    this.buf = new Float32Array(sampleRate * 4);
    this.head = 0;
    this.tail = 0;
    this.size = 0;
    this.port.onmessage = (e) => {
      const s = e.data, cap = this.buf.length;
      for (let i = 0; i < s.length; i++) {
        if (this.size >= cap) { this.head = (this.head + 1) % cap; this.size--; }
        this.buf[this.tail] = s[i];
        this.tail = (this.tail + 1) % cap;
        this.size++;
      }
    };
  }

  process(_inputs, outputs) {
    const out = outputs[0][0], cap = this.buf.length;
    for (let i = 0; i < out.length; i++) {
      if (this.size > 0) {
        out[i] = this.buf[this.head];
        this.head = (this.head + 1) % cap;
        this.size--;
      } else {
        out[i] = 0;
      }
    }
    for (let ch = 1; ch < outputs[0].length; ch++) outputs[0][ch].set(out);
    return true;
  }
}
registerProcessor('ring-player', RingPlayer);
