package com.vsq.phonvrstream.network

import com.vsq.phonvrstream.streaming.VideoFragmentReassembler

/** Keep valid fragment indexes and the -2 full-access-unit repair sentinel. */
internal fun filterVideoNackIndexes(indexes: List<Int>): List<Int> = indexes
    .asSequence()
    .filter { it >= 0 || it == VideoFragmentReassembler.WHOLE_FRAME_FRAGMENT_INDEX }
    .distinct()
    .take(256)
    .toList()
