package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sepiano/se"
)

// JoinPerformances makes one continuous performance of several, one after
// the other, so that they play without the pause it takes to upload the
// player again between pieces. Each keeps its own lead-in and ending, so the
// gaps between them are the ones the files were made with. It returns the
// tick at which each part starts.
//
// Key events are shifted in time; the damper and soft pedal samples are laid
// end to end. Each part is padded to a multiple of 16 ticks so that both
// pedal streams (sampled every 8 and every 16 ticks) stay aligned.
func JoinPerformances(parts []*se.Performance, text string) (*se.Performance, []uint32, error) {
	if len(parts) == 0 {
		return nil, nil, errors.New("nothing to join")
	}
	const align = se.SoftPeriod // a multiple of se.DamperPeriod
	first := parts[0].KB
	kb := &se.KB{Format: first.Format, Model: first.Model, Text: strings.ReplaceAll(text, "\x7f", "")}
	var damper, soft []byte
	starts := make([]uint32, len(parts))
	var offset uint32
	for i, p := range parts {
		starts[i] = offset
		end := p.KB.End
		for _, e := range p.KB.Notes() {
			if e.Tick > end {
				end = e.Tick
			}
			e.Tick += offset
			kb.Events = append(kb.Events, e)
		}
		end = (end + align - 1) / align * align
		damper = append(damper, fitSamples(p.Damper, int(end/se.DamperPeriod))...)
		soft = append(soft, fitSamples(p.Soft, int(end/se.SoftPeriod))...)
		offset += end
	}
	kb.End = offset
	return &se.Performance{KB: kb, Damper: &se.Pedal{Samples: damper}, Soft: &se.Pedal{Samples: soft}}, starts, nil
}

// fitSamples returns exactly n samples of a pedal stream: cut short, or held
// at its last position.
func fitSamples(p *se.Pedal, n int) []byte {
	var s []byte
	if p != nil {
		s = p.Samples
	}
	out := make([]byte, n)
	copy(out, s)
	if len(s) > 0 && len(s) < n {
		last := s[len(s)-1]
		for i := len(s); i < n; i++ {
			out[i] = last
		}
	}
	return out
}

// Part is one piece within a joined performance.
type Part struct {
	ID         string
	StartMs    int64
	DurationMs int64
}

// Fix changes one piece's performance before it is joined (the recording
// corrections), and reports whether it changed anything.
type Fix func(p Piece, perf *se.Performance) (*se.Performance, bool, error)

// Join writes the pieces, in order, as one performance dir/name.{kb,lp,sp}
// and returns where each piece starts in it. fix, if not nil, is given each
// piece first; fixed says whether it changed any.
func (l *Library) Join(ids []string, dir, name, title string, fix Fix) (parts []Part, fixed bool, err error) {
	perfs := make([]*se.Performance, len(ids))
	for i, id := range ids {
		piece, err := l.Get(id)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", id, err)
		}
		p, err := se.Load(filepath.Join(l.dir, id))
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", id, err)
		}
		if fix != nil {
			q, changed, err := fix(piece, p)
			if err != nil {
				return nil, false, fmt.Errorf("%s: %w", id, err)
			}
			if changed {
				p, fixed = q, true
			}
		}
		perfs[i] = p
	}
	joined, starts, err := JoinPerformances(perfs, title+"\r\n")
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, err
	}
	if err := joined.Save(filepath.Join(dir, name)); err != nil {
		return nil, false, err
	}
	parts = make([]Part, len(ids))
	for i, id := range ids {
		end := joined.KB.End
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		parts[i] = Part{ID: id, StartMs: se.TickDuration(starts[i]).Milliseconds(),
			DurationMs: se.TickDuration(end - starts[i]).Milliseconds()}
	}
	return parts, fixed, nil
}
