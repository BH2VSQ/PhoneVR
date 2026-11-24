#!/usr/bin/env python3
"""Apply the PhoneVR WaveRT capture patch to Microsoft's Windows-driver-samples SysVAD."""
from __future__ import annotations
import argparse
from pathlib import Path
import shutil
import sys


METHODS = r'''
// PhoneVR microphone bridge. The shared section is read only from this PASSIVE_LEVEL
// worker; WaveRT's timer/DPC consumes a private nonpaged byte queue.
NTSTATUS CMiniportWaveRTStream::PhoneVRMicStartWorker()
{
    if (!m_bCapture || !m_pMiniport || !m_pMiniport->IsPhoneVRMicrophone())
        return STATUS_SUCCESS;
    if (InterlockedCompareExchange(&m_PhoneVRMicWorkerRunning, 1, 0) != 0)
        return STATUS_SUCCESS;

    KIRQL oldIrql;
    KeAcquireSpinLock(&m_PhoneVRMicQueueLock, &oldIrql);
    m_PhoneVRMicQueueRead = 0;
    m_PhoneVRMicQueueWrite = 0;
    m_PhoneVRMicQueueCount = 0;
    KeReleaseSpinLock(&m_PhoneVRMicQueueLock, oldIrql);
    KeResetEvent(&m_PhoneVRMicStopEvent);
    HANDLE threadHandle = NULL;
    NTSTATUS status = PsCreateSystemThread(
        &threadHandle, THREAD_ALL_ACCESS, NULL, NULL, NULL,
        PhoneVRMicWorkerThread, this);
    if (!NT_SUCCESS(status)) {
        InterlockedExchange(&m_PhoneVRMicWorkerRunning, 0);
        return status;
    }

    PETHREAD threadObject = NULL;
    status = ObReferenceObjectByHandle(
        threadHandle, SYNCHRONIZE, *PsThreadType, KernelMode,
        reinterpret_cast<PVOID*>(&threadObject), NULL);
    if (!NT_SUCCESS(status)) {
        InterlockedExchange(&m_PhoneVRMicWorkerRunning, 0);
        KeSetEvent(&m_PhoneVRMicStopEvent, IO_NO_INCREMENT, FALSE);
        ZwWaitForSingleObject(threadHandle, FALSE, NULL);
        ZwClose(threadHandle);
        return status;
    }
    m_PhoneVRMicThreadObject = threadObject;
    ZwClose(threadHandle);
    return STATUS_SUCCESS;
}

VOID CMiniportWaveRTStream::PhoneVRMicStopWorker()
{
    InterlockedExchange(&m_PhoneVRMicWorkerRunning, 0);
    KeSetEvent(&m_PhoneVRMicStopEvent, IO_NO_INCREMENT, FALSE);
    if (m_PhoneVRMicThreadObject) {
        KeWaitForSingleObject(m_PhoneVRMicThreadObject, Executive, KernelMode, FALSE, NULL);
        ObDereferenceObject(m_PhoneVRMicThreadObject);
        m_PhoneVRMicThreadObject = NULL;
    }
}

BOOLEAN CMiniportWaveRTStream::PhoneVRMicPopShared(
    _Inout_ PHONEVR_MIC_SHARED_RING* Ring,
    _Out_writes_bytes_(PHONEVR_MIC_FRAME_BYTES) PUCHAR Frame)
{
    for (ULONG attempt = 0; attempt < 8; ++attempt) {
        const LONG64 position = InterlockedCompareExchange64(
            reinterpret_cast<volatile LONG64*>(&Ring->DequeuePosition), 0, 0);
        if (position < 0 || position > MAXLONGLONG - PHONEVR_MIC_RING_SLOT_COUNT)
            return FALSE;
        PHONEVR_MIC_RING_SLOT* slot = &Ring->Slots[position % PHONEVR_MIC_RING_SLOT_COUNT];
        const LONG64 sequence = InterlockedCompareExchange64(
            reinterpret_cast<volatile LONG64*>(&slot->Sequence), 0, 0);
        if (sequence < position + 1) return FALSE;
        if (sequence > position + 1) continue;
        if (InterlockedCompareExchange64(
                reinterpret_cast<volatile LONG64*>(&Ring->DequeuePosition),
                position + 1, position) != position)
            continue;
        RtlCopyMemory(Frame, slot->Payload, PHONEVR_MIC_FRAME_BYTES);
        KeMemoryBarrier();
        InterlockedExchange64(reinterpret_cast<volatile LONG64*>(&slot->Sequence),
                              position + PHONEVR_MIC_RING_SLOT_COUNT);
        return TRUE;
    }
    return FALSE;
}

VOID CMiniportWaveRTStream::PhoneVRMicQueuePush(
    _In_reads_bytes_(PHONEVR_MIC_FRAME_BYTES) PUCHAR Frame)
{
    KIRQL oldIrql;
    KeAcquireSpinLock(&m_PhoneVRMicQueueLock, &oldIrql);
    const ULONG capacity = static_cast<ULONG>(sizeof(m_PhoneVRMicQueue));
    const ULONG frameBytes = static_cast<ULONG>(PHONEVR_MIC_FRAME_BYTES);
    // Keep the queue sample-aligned and discard complete 20 ms units on
    // overflow. Removing individual bytes can shift the 16-bit PCM stream.
    while (capacity - m_PhoneVRMicQueueCount < frameBytes) {
        const ULONG dropBytes = frameBytes < m_PhoneVRMicQueueCount
            ? frameBytes : m_PhoneVRMicQueueCount;
        m_PhoneVRMicQueueRead = (m_PhoneVRMicQueueRead + dropBytes) % capacity;
        m_PhoneVRMicQueueCount -= dropBytes;
    }
    const ULONG tailBytes = capacity - m_PhoneVRMicQueueWrite;
    const ULONG first = frameBytes < tailBytes ? frameBytes : tailBytes;
    RtlCopyMemory(m_PhoneVRMicQueue + m_PhoneVRMicQueueWrite, Frame, first);
    const ULONG second = frameBytes - first;
    if (second) RtlCopyMemory(m_PhoneVRMicQueue, Frame + first, second);
    m_PhoneVRMicQueueWrite = (m_PhoneVRMicQueueWrite + frameBytes) % capacity;
    m_PhoneVRMicQueueCount += frameBytes;
    KeReleaseSpinLock(&m_PhoneVRMicQueueLock, oldIrql);
}

VOID NTAPI CMiniportWaveRTStream::PhoneVRMicWorkerThread(_In_ PVOID Context)
{
    auto* stream = static_cast<CMiniportWaveRTStream*>(Context);
    PVOID sectionObject = NULL;
    PVOID mappedView = NULL;
    SIZE_T viewSize = 0;
    UCHAR frame[PHONEVR_MIC_FRAME_BYTES];

    while (InterlockedCompareExchange(&stream->m_PhoneVRMicWorkerRunning, 1, 1) != 0) {
        if (!mappedView) {
            UNICODE_STRING sectionName;
            RtlInitUnicodeString(&sectionName, PHONEVR_MIC_SECTION_NT);
            OBJECT_ATTRIBUTES attributes;
            InitializeObjectAttributes(&attributes, &sectionName,
                OBJ_CASE_INSENSITIVE | OBJ_KERNEL_HANDLE, NULL, NULL);
            HANDLE sectionHandle = NULL;
            NTSTATUS status = ZwOpenSection(&sectionHandle, SECTION_MAP_READ | SECTION_MAP_WRITE, &attributes);
            if (NT_SUCCESS(status)) {
                status = ObReferenceObjectByHandle(sectionHandle, SECTION_MAP_READ | SECTION_MAP_WRITE,
                    *MmSectionObjectType, KernelMode, &sectionObject, NULL);
                ZwClose(sectionHandle);
                if (NT_SUCCESS(status)) {
                    viewSize = 0;
                    status = MmMapViewInSystemSpace(sectionObject, &mappedView, &viewSize);
                    if (!NT_SUCCESS(status)) {
                        ObDereferenceObject(sectionObject);
                        sectionObject = NULL;
                        mappedView = NULL;
                    }
                }
            }
            if (!mappedView) {
                LARGE_INTEGER retryDelay;
                retryDelay.QuadPart = -100 * 10000; // retry after 100 ms
                KeWaitForSingleObject(&stream->m_PhoneVRMicStopEvent, Executive,
                                      KernelMode, FALSE, &retryDelay);
                continue;
            }
        }

        if (viewSize >= sizeof(PHONEVR_MIC_SHARED_RING)) {
            auto* ring = static_cast<PHONEVR_MIC_SHARED_RING*>(mappedView);
            if (ring->Magic == PHONEVR_MIC_RING_MAGIC &&
                ring->Version == PHONEVR_MIC_RING_VERSION &&
                ring->FrameBytes == PHONEVR_MIC_FRAME_BYTES &&
                ring->SlotCount == PHONEVR_MIC_RING_SLOT_COUNT) {
                // The host may have been sending before a capture client opened
                // the endpoint. Drop all but the newest four queued frames (80 ms).
                const LONG64 enqueue = InterlockedCompareExchange64(
                    reinterpret_cast<volatile LONG64*>(&ring->EnqueuePosition), 0, 0);
                const LONG64 dequeue = InterlockedCompareExchange64(
                    reinterpret_cast<volatile LONG64*>(&ring->DequeuePosition), 0, 0);
                LONG64 stale = 0;
                if (enqueue >= dequeue && enqueue - dequeue > 4 &&
                    enqueue - dequeue <= PHONEVR_MIC_RING_SLOT_COUNT)
                    stale = enqueue - dequeue - 4;
                while (stale-- > 0 && stream->PhoneVRMicPopShared(ring, frame)) { }
                while (stream->PhoneVRMicPopShared(ring, frame))
                    stream->PhoneVRMicQueuePush(frame);
            }
        }

        LARGE_INTEGER pollDelay;
        pollDelay.QuadPart = -2 * 10000; // 2 ms, outside the WaveRT DPC
        KeWaitForSingleObject(&stream->m_PhoneVRMicStopEvent, Executive,
                              KernelMode, FALSE, &pollDelay);
    }

    if (mappedView) MmUnmapViewInSystemSpace(mappedView);
    if (sectionObject) ObDereferenceObject(sectionObject);
    PsTerminateSystemThread(STATUS_SUCCESS);
}

VOID CMiniportWaveRTStream::PhoneVRMicReadBytes(_In_ ULONG ByteDisplacement)
{
    ULONG bufferOffset = static_cast<ULONG>(m_ullLinearPosition % m_ulDmaBufferSize);
    while (ByteDisplacement > 0) {
        const ULONG runWrite = min(ByteDisplacement, m_ulDmaBufferSize - bufferOffset);
        const BOOLEAN atDpcLevel = (KeGetCurrentIrql() == DISPATCH_LEVEL);
        KIRQL oldIrql = PASSIVE_LEVEL;
        if (atDpcLevel) {
            // UpdatePosition can call WriteBytes while TimerNotifyRT already
            // holds a spin lock at DISPATCH_LEVEL. Do not raise IRQL again.
            KeAcquireSpinLockAtDpcLevel(&m_PhoneVRMicQueueLock);
        } else {
            KeAcquireSpinLock(&m_PhoneVRMicQueueLock, &oldIrql);
        }
        const ULONG copyBytes = min(runWrite, m_PhoneVRMicQueueCount);
        ULONG first = min(copyBytes, static_cast<ULONG>(sizeof(m_PhoneVRMicQueue) - m_PhoneVRMicQueueRead));
        if (first) {
            RtlCopyMemory(m_pDmaBuffer + bufferOffset,
                          m_PhoneVRMicQueue + m_PhoneVRMicQueueRead, first);
        }
        const ULONG second = copyBytes - first;
        if (second) {
            RtlCopyMemory(m_pDmaBuffer + bufferOffset + first,
                          m_PhoneVRMicQueue, second);
        }
        m_PhoneVRMicQueueRead = (m_PhoneVRMicQueueRead + copyBytes) % sizeof(m_PhoneVRMicQueue);
        m_PhoneVRMicQueueCount -= copyBytes;
        if (atDpcLevel) {
            KeReleaseSpinLockFromDpcLevel(&m_PhoneVRMicQueueLock);
        } else {
            KeReleaseSpinLock(&m_PhoneVRMicQueueLock, oldIrql);
        }
        if (copyBytes < runWrite)
            RtlZeroMemory(m_pDmaBuffer + bufferOffset + copyBytes, runWrite - copyBytes);
        bufferOffset = (bufferOffset + runWrite) % m_ulDmaBufferSize;
        ByteDisplacement -= runWrite;
    }
}
'''


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"expected exactly one {label} anchor, found {count}")
    return text.replace(old, new, 1)


def update(path: Path, fn) -> None:
    original = path.read_text(encoding="utf-8-sig")
    modified = fn(original)
    if modified != original:
        backup = path.with_suffix(path.suffix + ".phonevr.bak")
        if not backup.exists():
            shutil.copy2(path, backup)
        path.write_text(modified, encoding="utf-8", newline="")
        print(f"patched: {path}")
    else:
        print(f"already patched: {path}")


def patch_minwavert_h(text: str) -> str:
    if '#include "PhoneVRAudioShared.h"' not in text:
        text = replace_once(text,
            "#define _SYSVAD_MINWAVERT_H_",
            '#define _SYSVAD_MINWAVERT_H_\n\n#include "PhoneVRAudioShared.h"',
            "miniport shared ring include")
    if "IsPhoneVRMicrophone() const" not in text:
        text = replace_once(text,
            "public:\n    DECLARE_PROPERTYHANDLER(Get_SoundDetectorSupportedPatterns);",
            "public:\n    BOOLEAN IsPhoneVRMicrophone() const { return m_DeviceType == eMicInDevice; }\n    NTSTATUS PhoneVRMicCreateSharedSection();\n    VOID PhoneVRMicCloseSharedSection();\n\n    DECLARE_PROPERTYHANDLER(Get_SoundDetectorSupportedPatterns);",
            "miniport microphone helpers")
    elif "PhoneVRMicCreateSharedSection();" not in text:
        text = replace_once(text,
            "    BOOLEAN IsPhoneVRMicrophone() const { return m_DeviceType == eMicInDevice; }",
            "    BOOLEAN IsPhoneVRMicrophone() const { return m_DeviceType == eMicInDevice; }\n    NTSTATUS PhoneVRMicCreateSharedSection();\n    VOID PhoneVRMicCloseSharedSection();",
            "miniport section helper declarations")
    if "m_PhoneVRMicSectionHandle;" not in text:
        text = replace_once(text,
            "    PENDPOINT_MINIPAIR                  m_pMiniportPair;",
            "    PENDPOINT_MINIPAIR                  m_pMiniportPair;\n    HANDLE                              m_PhoneVRMicSectionHandle;",
            "miniport section handle")
    if "m_PhoneVRMicSectionHandle = NULL;" not in text:
        text = replace_once(text,
            "        m_pPortClsNotifications(NULL)\n    {\n        PAGED_CODE();",
            "        m_pPortClsNotifications(NULL)\n    {\n        PAGED_CODE();\n        m_PhoneVRMicSectionHandle = NULL;",
            "miniport section handle constructor initialization")
    return text


MINIPORT_SECTION_IMPL = r"""#pragma code_seg("PAGE")
NTSTATUS CMiniportWaveRT::PhoneVRMicCreateSharedSection()
{
    PAGED_CODE();
    if (!IsPhoneVRMicrophone()) return STATUS_SUCCESS;
    if (m_PhoneVRMicSectionHandle) return STATUS_SUCCESS;

    SID_IDENTIFIER_AUTHORITY ntAuthority = SECURITY_NT_AUTHORITY;
    PSID systemSid = NULL;
    PSID interactiveSid = NULL;
    PACL acl = NULL;
    NTSTATUS status = RtlAllocateAndInitializeSid(
        &ntAuthority, 1, SECURITY_LOCAL_SYSTEM_RID, 0, 0, 0, 0, 0, 0, 0, &systemSid);
    if (!NT_SUCCESS(status)) return status;
    status = RtlAllocateAndInitializeSid(
        &ntAuthority, 1, SECURITY_INTERACTIVE_RID, 0, 0, 0, 0, 0, 0, 0, &interactiveSid);
    if (!NT_SUCCESS(status)) {
        RtlFreeSid(systemSid);
        return status;
    }

    const ULONG aclBytes = static_cast<ULONG>(sizeof(ACL) +
        2 * (sizeof(ACCESS_ALLOWED_ACE) - sizeof(ULONG)) +
        RtlLengthSid(systemSid) + RtlLengthSid(interactiveSid));
    acl = static_cast<PACL>(ExAllocatePool2(POOL_FLAG_PAGED, aclBytes, MINWAVERT_POOLTAG));
    if (!acl) {
        RtlFreeSid(interactiveSid);
        RtlFreeSid(systemSid);
        return STATUS_INSUFFICIENT_RESOURCES;
    }
    status = RtlCreateAcl(acl, aclBytes, ACL_REVISION);
    const ACCESS_MASK sectionAccess = SECTION_MAP_READ | SECTION_MAP_WRITE | SECTION_QUERY;
    if (NT_SUCCESS(status)) status = RtlAddAccessAllowedAce(acl, ACL_REVISION, sectionAccess, systemSid);
    if (NT_SUCCESS(status)) status = RtlAddAccessAllowedAce(acl, ACL_REVISION, sectionAccess, interactiveSid);

    SECURITY_DESCRIPTOR securityDescriptor;
    if (NT_SUCCESS(status)) status = RtlCreateSecurityDescriptor(&securityDescriptor, SECURITY_DESCRIPTOR_REVISION);
    if (NT_SUCCESS(status)) status = RtlSetDaclSecurityDescriptor(&securityDescriptor, TRUE, acl, FALSE);

    HANDLE sectionHandle = NULL;
    if (NT_SUCCESS(status)) {
        UNICODE_STRING sectionName;
        RtlInitUnicodeString(&sectionName, PHONEVR_MIC_SECTION_NT);
        OBJECT_ATTRIBUTES attributes;
        InitializeObjectAttributes(&attributes, &sectionName,
            OBJ_CASE_INSENSITIVE | OBJ_KERNEL_HANDLE, NULL, &securityDescriptor);
        LARGE_INTEGER maximumSize;
        maximumSize.QuadPart = sizeof(PHONEVR_MIC_SHARED_RING);
        status = ZwCreateSection(&sectionHandle, SECTION_MAP_READ | SECTION_MAP_WRITE | SECTION_QUERY, &attributes,
            &maximumSize, PAGE_READWRITE, SEC_COMMIT, NULL);
        if (status == STATUS_OBJECT_NAME_COLLISION) {
            status = ZwOpenSection(&sectionHandle, SECTION_MAP_READ | SECTION_MAP_WRITE, &attributes);
        }
    }

    ExFreePoolWithTag(acl, MINWAVERT_POOLTAG);
    RtlFreeSid(interactiveSid);
    RtlFreeSid(systemSid);
    if (!NT_SUCCESS(status)) return status;

    PVOID sectionObject = NULL;
    status = ObReferenceObjectByHandle(sectionHandle, SECTION_MAP_READ | SECTION_MAP_WRITE,
        *MmSectionObjectType, KernelMode, &sectionObject, NULL);
    if (!NT_SUCCESS(status)) {
        ZwClose(sectionHandle);
        return status;
    }

    PVOID view = NULL;
    SIZE_T viewSize = 0;
    status = MmMapViewInSystemSpace(sectionObject, &view, &viewSize);
    if (NT_SUCCESS(status)) {
        if (viewSize < sizeof(PHONEVR_MIC_SHARED_RING)) {
            status = STATUS_BUFFER_TOO_SMALL;
        } else {
            auto* ring = static_cast<PHONEVR_MIC_SHARED_RING*>(view);
            if (ring->Magic != PHONEVR_MIC_RING_MAGIC ||
                ring->Version != PHONEVR_MIC_RING_VERSION ||
                ring->FrameBytes != PHONEVR_MIC_FRAME_BYTES ||
                ring->SlotCount != PHONEVR_MIC_RING_SLOT_COUNT) {
                RtlZeroMemory(ring, sizeof(*ring));
                ring->FrameBytes = PHONEVR_MIC_FRAME_BYTES;
                ring->SlotCount = PHONEVR_MIC_RING_SLOT_COUNT;
                ring->EnqueuePosition = 0;
                ring->DequeuePosition = 0;
                for (ULONG i = 0; i < PHONEVR_MIC_RING_SLOT_COUNT; ++i)
                    ring->Slots[i].Sequence = i;
                KeMemoryBarrier();
                ring->Version = PHONEVR_MIC_RING_VERSION;
                ring->Magic = PHONEVR_MIC_RING_MAGIC;
            }
        }
        MmUnmapViewInSystemSpace(view);
    }
    ObDereferenceObject(sectionObject);
    if (!NT_SUCCESS(status)) {
        ZwClose(sectionHandle);
        return status;
    }

    m_PhoneVRMicSectionHandle = sectionHandle;
    DPF(D_VERBOSE, ("PhoneVR microphone shared audio section is ready"));
    return STATUS_SUCCESS;
}

#pragma code_seg("PAGE")
VOID CMiniportWaveRT::PhoneVRMicCloseSharedSection()
{
    PAGED_CODE();
    if (m_PhoneVRMicSectionHandle) {
        ZwClose(m_PhoneVRMicSectionHandle);
        m_PhoneVRMicSectionHandle = NULL;
    }
}
"""


def patch_miniport_cpp(text: str) -> str:
    if "CMiniportWaveRT::PhoneVRMicCreateSharedSection()" in text:
        return text
    init_start = text.index("CMiniportWaveRT::Init")
    init_paged = text.index("    PAGED_CODE();", init_start)
    pos = init_paged + len("    PAGED_CODE();")
    text = text[:pos] + "\n    m_PhoneVRMicSectionHandle = NULL;" + text[pos:]

    destructor_start = text.index("CMiniportWaveRT::~CMiniportWaveRT")
    destructor_paged = text.index("    PAGED_CODE();", destructor_start)
    pos = destructor_paged + len("    PAGED_CODE();")
    text = text[:pos] + "\n    PhoneVRMicCloseSharedSection();" + text[pos:]

    text = replace_once(text,
        "    return ntStatus;\n} // Init",
        "    if (NT_SUCCESS(ntStatus) && IsPhoneVRMicrophone()) {\n        ntStatus = PhoneVRMicCreateSharedSection();\n    }\n\n    return ntStatus;\n} // Init",
        "miniport shared section initialization")
    marker = "//=============================================================================\n#pragma code_seg(\"PAGE\")\nSTDMETHODIMP_(NTSTATUS)\nCMiniportWaveRT::NewStream"
    return replace_once(text, marker,
        "//=============================================================================\n#pragma code_seg(\"PAGE\")\n" + MINIPORT_SECTION_IMPL + "\n//=============================================================================\n#pragma code_seg(\"PAGE\")\nSTDMETHODIMP_(NTSTATUS)\nCMiniportWaveRT::NewStream",
        "miniport shared section implementation")


def patch_stream_h(text: str) -> str:
    if "PhoneVRMicWorkerThread" in text:
        return text
    text = replace_once(text,
        '#include "tonegenerator.h"',
        '#include "tonegenerator.h"\n#include "PhoneVRAudioShared.h"',
        "shared ring include")
    text = replace_once(text,
        "    ULONG                       m_AudioModuleCount;",
        """    ULONG                       m_AudioModuleCount;
    volatile LONG                m_PhoneVRMicWorkerRunning;
    PETHREAD                      m_PhoneVRMicThreadObject;
    KEVENT                        m_PhoneVRMicStopEvent;
    KSPIN_LOCK                    m_PhoneVRMicQueueLock;
    ULONG                         m_PhoneVRMicQueueRead;
    ULONG                         m_PhoneVRMicQueueWrite;
    ULONG                         m_PhoneVRMicQueueCount;
    UCHAR                         m_PhoneVRMicQueue[PHONEVR_MIC_FRAME_BYTES * 8];""",
        "WaveRT stream fields")
    return replace_once(text,
        "    NTSTATUS ReadRegistrySettings();",
        """    NTSTATUS ReadRegistrySettings();
    NTSTATUS PhoneVRMicStartWorker();
    VOID PhoneVRMicStopWorker();
    static VOID NTAPI PhoneVRMicWorkerThread(_In_ PVOID Context);
    BOOLEAN PhoneVRMicPopShared(_Inout_ PHONEVR_MIC_SHARED_RING* Ring,
        _Out_writes_bytes_(PHONEVR_MIC_FRAME_BYTES) PUCHAR Frame);
    VOID PhoneVRMicQueuePush(_In_reads_bytes_(PHONEVR_MIC_FRAME_BYTES) PUCHAR Frame);
    VOID PhoneVRMicReadBytes(_In_ ULONG ByteDisplacement);""",
        "WaveRT stream helper declarations")


def patch_stream_cpp(text: str) -> str:
    if "PhoneVRMicStartWorker()" in text:
        return text
    text = replace_once(text,
        "    PAGED_CODE();\n\n    PWAVEFORMATEX pWfEx = NULL;",
        """    PAGED_CODE();
    m_PhoneVRMicWorkerRunning = 0;
    m_PhoneVRMicThreadObject = NULL;
    m_PhoneVRMicQueueRead = 0;
    m_PhoneVRMicQueueWrite = 0;
    m_PhoneVRMicQueueCount = 0;
    KeInitializeEvent(&m_PhoneVRMicStopEvent, NotificationEvent, FALSE);
    KeInitializeSpinLock(&m_PhoneVRMicQueueLock);

    PWAVEFORMATEX pWfEx = NULL;""",
        "stream initialization")
    destructor_start = text.index("CMiniportWaveRTStream::~CMiniportWaveRTStream")
    destructor_body = text.index("    PAGED_CODE();", destructor_start)
    after_paged = destructor_body + len("    PAGED_CODE();")
    text = text[:after_paged] + "\n    PhoneVRMicStopWorker();" + text[after_paged:]
    text = replace_once(text,
        "    switch (State_)\n    {",
        """    if (m_bCapture && m_pMiniport && m_pMiniport->IsPhoneVRMicrophone()) {
        if (State_ == KSSTATE_RUN) {
            NTSTATUS micStatus = PhoneVRMicStartWorker();
            if (!NT_SUCCESS(micStatus))
                DPF(D_ERROR, (\"PhoneVR microphone worker start failed: 0x%x\", micStatus));
        } else {
            PhoneVRMicStopWorker();
        }
    }

    switch (State_)
    {""",
        "WaveRT state transition")
    # WriteBytes is the WaveRT capture-buffer producer in this SysVAD sample.
    # ReadBytes is a separate render/debug-save helper with a similar prologue,
    # so scope the replacement to WriteBytes rather than a global text anchor.
    write_start = text.index("VOID CMiniportWaveRTStream::WriteBytes")
    write_end = text.find("VOID CMiniportWaveRTStream::ReadBytes", write_start)
    if write_end < 0:
        raise RuntimeError("WaveRT ReadBytes boundary was not found after WriteBytes")
    write_function = text[write_start:write_end]
    write_function = replace_once(write_function,
        "{\n    ULONG bufferOffset = m_ullLinearPosition % m_ulDmaBufferSize;\n\n    // Normally this will loop no more than once for a single wrap, but if\n    // many bytes have been displaced then this may loops many times.\n    while (ByteDisplacement > 0)",
        "{\n    if (m_bCapture && m_pMiniport && m_pMiniport->IsPhoneVRMicrophone()) {\n        PhoneVRMicReadBytes(ByteDisplacement);\n        return;\n    }\n    ULONG bufferOffset = m_ullLinearPosition % m_ulDmaBufferSize;\n\n    // Normally this will loop no more than once for a single wrap, but if\n    // many bytes have been displaced then this may loops many times.\n    while (ByteDisplacement > 0)",
        "WaveRT microphone capture data source")
    text = text[:write_start] + write_function + text[write_end:]
    return replace_once(text,
        "//=============================================================================\n#pragma code_seg()\nVOID CMiniportWaveRTStream::WriteBytes",
        "//=============================================================================\n#pragma code_seg()\n" + METHODS + "\n//=============================================================================\n#pragma code_seg()\nVOID CMiniportWaveRTStream::WriteBytes",
        "WaveRT microphone worker implementation")


def patch_minipairs(text: str) -> str:
    # The pinned Microsoft sample has trailing whitespace after the array
    # declarators. Normalize only these two known anchors before matching.
    text = text.replace("g_RenderEndpoints[] = \n", "g_RenderEndpoints[] =\n")
    text = text.replace("g_CaptureEndpoints[] = \n", "g_CaptureEndpoints[] =\n")
    render_original = "static\nPENDPOINT_MINIPAIR  g_RenderEndpoints[] =\n{\n    &SpeakerMiniports,\n    &SpeakerHpMiniports,\n    &HdmiMiniports,\n    &SpdifMiniports,\n};\n\n#define g_cRenderEndpoints  (SIZEOF_ARRAY(g_RenderEndpoints))"
    render_patched = "static\nPENDPOINT_MINIPAIR  g_RenderEndpoints[] =\n{\n    NULL, // This package intentionally exposes no render endpoint.\n};\n\n#define g_cRenderEndpoints  0"
    if render_original in text:
        text = replace_once(text, render_original, render_patched, "render endpoint list")
    elif render_patched not in text:
        raise RuntimeError("render endpoint list is neither original nor already patched")

    capture_original = "static\nPENDPOINT_MINIPAIR  g_CaptureEndpoints[] =\n{\n    &MicInMiniports,\n    &MicArray1Miniports,\n    &MicArray2Miniports,\n    &MicArray3Miniports,\n};"
    capture_patched = "static\nPENDPOINT_MINIPAIR  g_CaptureEndpoints[] =\n{\n    &MicInMiniports,\n};"
    if capture_original in text:
        text = replace_once(text, capture_original, capture_patched, "capture endpoint list")
    elif capture_patched not in text:
        raise RuntimeError("capture endpoint list is neither original nor already patched")
    return text


def patch_micinwav(text: str) -> str:
    # PhoneVR sends fixed 20 ms frames of 48 kHz mono PCM16. Expose exactly
    # this format so Windows cannot open a 16/24 kHz capture stream and consume
    # the fixed-size transport frames at the wrong rate.
    if "#define MICIN_MAX_INPUT_STREAMS             5" in text:
        text = replace_once(text,
            "#define MICIN_MAX_INPUT_STREAMS             5",
            "#define MICIN_MAX_INPUT_STREAMS             1",
            "single PhoneVR capture stream")
    if "#define MICIN_MAX_INPUT_STREAMS             1" not in text:
        raise RuntimeError("PhoneVR microphone must expose one underlying capture stream")

    if "#define MICIN_MIN_SAMPLE_RATE               8000" in text:
        text = replace_once(text, "#define MICIN_MIN_SAMPLE_RATE               8000",
                            "#define MICIN_MIN_SAMPLE_RATE               48000", "mic input minimum sample rate")
    if "#define MICIN_MIN_SAMPLE_RATE               48000" not in text or "MICIN_MIN_SAMPLE_RATE               8000" in text:
        raise RuntimeError("mic input format patch did not apply")

    table_marker = "KSDATAFORMAT_WAVEFORMATEXTENSIBLE MicInPinSupportedDeviceFormats[] ="
    end_marker = "\n\n//\n// Supported modes"
    table_name = text.find(table_marker)
    start = text.rfind("static", 0, table_name) if table_name >= 0 else -1
    end = text.find(end_marker, table_name + len(table_marker)) if table_name >= 0 else -1
    if start >= 0 and text[start:table_name] not in ("static\n", "static \n"):
        start = -1
    if start < 0 or end < 0:
        raise RuntimeError("could not locate the MicIn supported-format table")
    current = text[start:end]
    format_count = current.count("WAVE_FORMAT_EXTENSIBLE")
    if format_count == 8:
        only_48k = r'''static
KSDATAFORMAT_WAVEFORMATEXTENSIBLE MicInPinSupportedDeviceFormats[] =
{
    {
        {
            sizeof(KSDATAFORMAT_WAVEFORMATEXTENSIBLE),
            0,
            0,
            0,
            STATICGUIDOF(KSDATAFORMAT_TYPE_AUDIO),
            STATICGUIDOF(KSDATAFORMAT_SUBTYPE_PCM),
            STATICGUIDOF(KSDATAFORMAT_SPECIFIER_WAVEFORMATEX)
        },
        {
            {
                WAVE_FORMAT_EXTENSIBLE,
                1,
                48000,
                96000,
                2,
                16,
                sizeof(WAVEFORMATEXTENSIBLE) - sizeof(WAVEFORMATEX)
            },
            16,
            KSAUDIO_SPEAKER_MONO,
            STATICGUIDOF(KSDATAFORMAT_SUBTYPE_PCM)
        }
    }
};'''
        text = text[:start] + only_48k + text[end:]
    elif format_count != 1:
        raise RuntimeError(f"expected the original 8 microphone formats or the patched 48 kHz format, found {format_count}")

    text = text.replace("&MicInPinSupportedDeviceFormats[2].DataFormat", "&MicInPinSupportedDeviceFormats[0].DataFormat")
    text = text.replace("&MicInPinSupportedDeviceFormats[4].DataFormat", "&MicInPinSupportedDeviceFormats[0].DataFormat")
    if "&MicInPinSupportedDeviceFormats[2].DataFormat" in text or "&MicInPinSupportedDeviceFormats[4].DataFormat" in text:
        raise RuntimeError("speech/communications modes still reference removed microphone formats")
    return text


def patch_inf(text: str) -> str:
    text = text.replace('SYSVAD.WaveMicIn.szPname="SYSVAD Wave Microphone Headphone"',
                        'SYSVAD.WaveMicIn.szPname="PhoneVR Microphone"')
    text = text.replace('SYSVAD.TopologyMicIn.szPname="SYSVAD Topology Microphone Headphone"',
                        'SYSVAD.TopologyMicIn.szPname="PhoneVR Microphone Topology"')
    text = text.replace('MicInCustomName= "External Microphone Headphone"',
                        'MicInCustomName= "PhoneVR Microphone"')
    if 'SYSVAD.WaveMicIn.szPname="PhoneVR Microphone"' not in text:
        raise RuntimeError("Could not locate the SysVAD microphone endpoint friendly-name setting")
    return text


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("sysvad_root", type=Path, help="Path to Windows-driver-samples/audio/sysvad")
    args = parser.parse_args()
    root: Path = args.sysvad_root.resolve()
    endpoints = root / "EndpointsCommon"
    tablet = root / "TabletAudioSample"
    required = [endpoints / "minwavert.h", endpoints / "minwavert.cpp", endpoints / "minwavertstream.h",
                endpoints / "minwavertstream.cpp", tablet / "micinwavtable.h",
                tablet / "ComponentizedAudioSample.inx", tablet / "minipairs.h"]
    missing = [str(p) for p in required if not p.is_file()]
    if missing:
        print("SysVAD source tree is incomplete; missing:\n  " + "\n  ".join(missing), file=sys.stderr)
        return 2

    shared = Path(__file__).parent / "shared" / "PhoneVRAudioShared.h"
    shutil.copy2(shared, endpoints / "PhoneVRAudioShared.h")
    update(endpoints / "minwavert.h", patch_minwavert_h)
    update(endpoints / "minwavert.cpp", patch_miniport_cpp)
    update(endpoints / "minwavertstream.h", patch_stream_h)
    update(endpoints / "minwavertstream.cpp", patch_stream_cpp)
    update(tablet / "minipairs.h", patch_minipairs)
    update(tablet / "micinwavtable.h", patch_micinwav)
    update(tablet / "ComponentizedAudioSample.inx", patch_inf)
    print("PhoneVR SysVAD microphone integration applied.")
    print("The TabletAudioSample capture endpoint is now named 'PhoneVR Microphone'.")
    print("Build and test-sign the driver with Visual Studio + Windows Driver Kit before installing it.")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
