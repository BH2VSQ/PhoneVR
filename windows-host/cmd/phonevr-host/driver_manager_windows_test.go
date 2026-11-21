//go:build windows

package main

import (
	"testing"
	"unsafe"
)

func TestNativeOpenFileNameWLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		if got := unsafe.Sizeof(nativeOpenFileName{}); got != 168 {
			t.Fatalf("OPENFILENAMEW amd64 size = %d, want 168", got)
		}
		checks := []struct {
			name string
			got  uintptr
			want uintptr
		}{
			{"EditInfo", unsafe.Offsetof(nativeOpenFileName{}.EditInfo), 136},
			{"Prompt", unsafe.Offsetof(nativeOpenFileName{}.Prompt), 144},
			{"Reserved", unsafe.Offsetof(nativeOpenFileName{}.Reserved), 152},
			{"Reserved2", unsafe.Offsetof(nativeOpenFileName{}.Reserved2), 160},
			{"FlagsEx", unsafe.Offsetof(nativeOpenFileName{}.FlagsEx), 164},
		}
		for _, check := range checks {
			if check.got != check.want {
				t.Errorf("OPENFILENAMEW.%s offset = %d, want %d", check.name, check.got, check.want)
			}
		}
	} else if unsafe.Sizeof(uintptr(0)) == 4 {
		if got := unsafe.Sizeof(nativeOpenFileName{}); got != 88 {
			t.Fatalf("OPENFILENAMEW 386 size = %d, want 88", got)
		}
	}
}
