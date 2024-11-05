// Copyright 2024 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package fuzzer

import (
	"sync"

	"github.com/google/syzkaller/pkg/signal"
	"github.com/google/syzkaller/pkg/stat"
)

// Cover keeps track of the signal known to the fuzzer.
type Cover struct {
	mu        sync.RWMutex
	maxSignal signal.Signal // max signal ever observed (including flakes)
	newSignal signal.Signal // newly identified max signal
}

func newCover() *Cover {
	cover := new(Cover)
	stat.New("max signal", "Maximum fuzzing signal (including flakes)",
		stat.Graph("signal"), stat.LenOf(&cover.maxSignal, &cover.mu))
	return cover
}

// Signal that should no longer be chased after.
// It is not returned in GrabSignalDelta().
func (cover *Cover) AddMaxSignal(sign signal.Signal) {
	cover.mu.Lock()
	defer cover.mu.Unlock()
	cover.maxSignal.Merge(sign)
}

func filterFsSignal(signal []uint64) []uint64 {
	filtered := make([]uint64, 0, len(signal))
	for _, cover := range signal {
		if cover >= 0xffffffff81f7db10 && cover <= 0xffffffff81fb3e69 {
			filtered = append(filtered, cover)
		}
	}

	return filtered
}

func (cover *Cover) addRawMaxSignal(signal []uint64, prio uint8) signal.Signal {
	cover.mu.Lock()
	defer cover.mu.Unlock()
	fs_signal := filterFsSignal(signal)
	diff := cover.maxSignal.DiffRaw(fs_signal, prio)
	if diff.Empty() {
		return diff
	}
	cover.maxSignal.Merge(diff)
	cover.newSignal.Merge(diff)
	return diff
}

func (cover *Cover) CopyMaxSignal() signal.Signal {
	cover.mu.RLock()
	defer cover.mu.RUnlock()
	return cover.maxSignal.Copy()
}

func (cover *Cover) GrabSignalDelta() signal.Signal {
	cover.mu.Lock()
	defer cover.mu.Unlock()
	plus := cover.newSignal
	cover.newSignal = nil
	return plus
}
