#ifndef OWLET_TUTK_H
#define OWLET_TUTK_H
#include <stdint.h>

/* Subset of the ThroughTek Kalay ABI we call. Struct layouts mirror the stock
 * IOTCAPIs.h / AVAPIs.h (natural alignment). */

typedef struct {
    uint32_t cb;
    uint32_t authentication_type;
    char     auth_key[8];
    uint32_t timeout;
} St_IOTCConnectInput;

typedef struct {
    uint32_t    cb;
    uint32_t    iotc_session_id;
    uint8_t     iotc_channel_id;
    uint32_t    timeout_sec;
    const char *account_or_identity;
    const char *password_or_token;
    int32_t     resend;
    uint32_t    security_mode;
    uint32_t    auth_type;
    int32_t     sync_recv_data;
} AVClientStartInConfig;

typedef struct {
    uint32_t cb;
    uint32_t server_type;
    int32_t  resend;
    int32_t  two_way_streaming;
    int32_t  sync_recv_data;
    uint32_t security_mode;
} AVClientStartOutConfig;

typedef struct {
    uint16_t codec_id;
    uint8_t  flags;
    uint8_t  cam_index;
    uint8_t  onlineNum;
    uint8_t  reserve[3];
    uint32_t timestamp;
    uint32_t video_width;
    uint32_t video_height;
} FrameInfo;

int  TUTK_SDK_Set_License_Key(const char *key);
int  TUTK_SDK_Set_Region(int region);
int  IOTC_Initialize2(uint16_t udp_port);
int  IOTC_Set_LanSearchPort(int port);
int  avInitialize(int max_channels);
int  IOTC_Get_SessionID(void);
int  IOTC_Connect_ByUIDEx(const char *uid, int session_id, St_IOTCConnectInput *in);
int  avClientStartEx(AVClientStartInConfig *in, AVClientStartOutConfig *out);
int  avSendIOCtrl(int av_index, unsigned int type, const char *data, int len);
int  avRecvFrameData2(int av_index, char *frame, int frame_len, int *actual,
                      int *expected, char *info, int info_len, int *info_actual,
                      unsigned int *frame_index);
int  avRecvAudioData(int av_index, char *frame, int frame_len, char *info,
                     int info_len, unsigned int *frame_index);
void avSendIOCtrlExit(int av_index);
void avClientStop(int av_index);
void IOTC_Session_Close(int session_id);
void avDeInitialize(void);
void IOTC_DeInitialize(void);

#endif
