#pragma once

#include <stddef.h>
#include <stdint.h>

#define PHONEVR_MIC_RING_MAGIC       0x4D525650u /* PVRM */
#define PHONEVR_MIC_RING_VERSION     1u
#define PHONEVR_MIC_SAMPLE_RATE      48000u
#define PHONEVR_MIC_FRAME_SAMPLES    960u
#define PHONEVR_MIC_FRAME_BYTES      (PHONEVR_MIC_FRAME_SAMPLES * sizeof(int16_t))
#define PHONEVR_MIC_RING_SLOT_COUNT  64u
#define PHONEVR_MIC_SECTION_WIN32    L"Global\\PhoneVR.Audio.UplinkV1"
#define PHONEVR_MIC_SECTION_NT       L"\\BaseNamedObjects\\Global\\PhoneVR.Audio.UplinkV1"

#if defined(_MSC_VER)
#define PHONEVR_ALIGN8 __declspec(align(8))
#else
#define PHONEVR_ALIGN8 __attribute__((aligned(8)))
#endif

typedef struct PHONEVR_ALIGN8 PHONEVR_MIC_RING_SLOT {
    volatile int64_t Sequence;
    uint8_t Payload[PHONEVR_MIC_FRAME_BYTES];
} PHONEVR_MIC_RING_SLOT;

typedef struct PHONEVR_ALIGN8 PHONEVR_MIC_SHARED_RING {
    uint32_t Magic;
    uint32_t Version;
    uint32_t FrameBytes;
    uint32_t SlotCount;
    volatile int64_t EnqueuePosition;
    volatile int64_t DequeuePosition;
    PHONEVR_MIC_RING_SLOT Slots[PHONEVR_MIC_RING_SLOT_COUNT];
} PHONEVR_MIC_SHARED_RING;

#if defined(__cplusplus)
static_assert(sizeof(PHONEVR_MIC_RING_SLOT) == 8u + PHONEVR_MIC_FRAME_BYTES, "PhoneVR shared slot ABI mismatch");
static_assert(offsetof(PHONEVR_MIC_SHARED_RING, Slots) % 8u == 0u, "PhoneVR shared ring alignment mismatch");
static_assert(sizeof(PHONEVR_MIC_SHARED_RING) == 32u + PHONEVR_MIC_RING_SLOT_COUNT * sizeof(PHONEVR_MIC_RING_SLOT),
              "PhoneVR shared ring size mismatch");
#endif
