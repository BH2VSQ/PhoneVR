package com.vsq.phonvrstream.network

import com.vsq.phonvrstream.streaming.VideoFragmentReassembler
import org.junit.Assert.assertEquals
import org.junit.Test

class VideoNackProtocolTest {
    @Test fun preservesFullFrameRepairSentinelAndFiltersInvalidIndexes() {
        assertEquals(
            listOf(VideoFragmentReassembler.WHOLE_FRAME_FRAGMENT_INDEX, 0, 4),
            filterVideoNackIndexes(listOf(-2, -1, 0, 4, -3, 0)),
        )
    }

    @Test fun capsNackIndexesAtProtocolLimit() {
        val indexes = filterVideoNackIndexes((-2..300).toList())
        assertEquals(256, indexes.size)
        assertEquals(-2, indexes.first())
        assertEquals(254, indexes.last())
    }
}
