// Copyright 2024 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package corpus

import (
	"math/rand"
	"sort"
	"sync"

	"github.com/google/syzkaller/pkg/log"
	"github.com/google/syzkaller/pkg/signal"
	"github.com/google/syzkaller/prog"
)

type ProgramsList struct {
	mu       sync.RWMutex
	progs    []*prog.Prog
	sumPrios int64
	accPrios []int64
}

func (pl *ProgramsList) ChooseProgram(r *rand.Rand) *prog.Prog {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	if len(pl.progs) == 0 {
		return nil
	}
	randVal := r.Int63n(pl.sumPrios + 1)
	idx := sort.Search(len(pl.accPrios), func(i int) bool {
		return pl.accPrios[i] >= randVal
	})
	return pl.progs[idx]
}

func (pl *ProgramsList) Programs() []*prog.Prog {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	return pl.progs
}

func (pl *ProgramsList) saveProgram(p *prog.Prog, signal signal.Signal) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	io_uring_flag := false
	for cover := range signal {
		// io_uring
		if cover >= 0xffffffff81ba06ed && cover <= 0xffffffff81bd1636 {
			io_uring_flag = true
		}
	}

	prio := int64(len(signal))
	if prio == 0 {
		prio = 1
	}

	if io_uring_flag {
		prio *= 100000
		log.Logf(0, "----- io_uring program found: %v, prio=%v", p, prio)

	}

	pl.sumPrios += prio
	pl.accPrios = append(pl.accPrios, pl.sumPrios)
	pl.progs = append(pl.progs, p)
}

func (pl *ProgramsList) replace(other *ProgramsList) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.sumPrios = other.sumPrios
	pl.accPrios = other.accPrios
	pl.progs = other.progs
}
