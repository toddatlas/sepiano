package deck

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// What a console line from the player or recorder means to the deck.
type consoleEvent int

const (
	evNone consoleEvent = iota
	evStart
	evPause
	evResume
	evQuit
	evRestart
	evEnd
	evLevel
	evArmed
	evRecording
	evInterlock
	evRecEnd     // the recorder is writing its files
	evDiscarded  // the recorder discarded the recording and is ready again
	evRecRefused // the table has no recording type or serial number
)

var levelRE = regexp.MustCompile(`^LEVEL=\s*(-?\d+)`)

// parseLine classifies one console line. "START PLAYING", "END OF
// PERFORMANCE", "QUIT PLAYING" and "LEVEL=0 (NORMAL)" / "LEVEL=-n" (down to
// -16) were printed by the real player on the piano; the pause, resume,
// restart strings come from the program text. The recorder's come from its
// code (docs/SUITE-PROGRAMS.md): it prints all three prompts when armed and
// confirms the start with "START RECORDING". Lines are matched by prefix,
// because the key help ("DEPRESS SPACE BAR TO STOP/RESUME PLAYING",
// "DEPRESS ENTER TO END RECORDING") mentions the same words.
func parseLine(line string) (consoleEvent, int) {
	s := strings.TrimSpace(strings.ToUpper(line))
	switch {
	case strings.HasPrefix(s, "START PLAYING"):
		return evStart, 0
	case strings.HasPrefix(s, "STOP PLAYING"):
		return evPause, 0
	case strings.HasPrefix(s, "RESUME PLAYING"):
		return evResume, 0
	case strings.HasPrefix(s, "QUIT PLAYING"):
		return evQuit, 0
	case strings.HasPrefix(s, "RESTART PERFORMANCE"):
		return evRestart, 0
	case strings.HasPrefix(s, "END OF PERFORMANCE"):
		return evEnd, 0
	case strings.Contains(s, "INTERLOCK FAILURE"):
		return evInterlock, 0
	case strings.Contains(s, "SPACE BAR TO START RECORDING"):
		return evArmed, 0
	case strings.HasPrefix(s, "START RECORDING"):
		return evRecording, 0
	case strings.HasPrefix(s, "END OF RECORDING"):
		return evRecEnd, 0
	case strings.HasPrefix(s, "DISCARD RECORDING"):
		return evDiscarded, 0
	case strings.Contains(s, "ILLEGAL RECORDING TYPE OR MODE"):
		return evRecRefused, 0
	}
	if m := levelRE.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			return evLevel, n
		}
	}
	return evNone, 0
}

// lineWriter splits the console byte stream into lines for the deck.
type lineWriter struct {
	buf  []byte
	emit func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexByte(string(w.buf), '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]
		if strings.TrimSpace(line) != "" {
			w.emit(line)
		}
	}
	if len(w.buf) > 4096 { // a runaway line without an end
		w.emit(string(w.buf))
		w.buf = w.buf[:0]
	}
	return len(p), nil
}

// recordingHeader is the header text the recorder writes into a recording:
// it asks the host for its command-line argument, as record.exe's original
// host passes it. The title and the date, in plain ASCII (the player prints
// it at the start), with "\" separating the lines.
func recordingHeader(title string, t time.Time) string {
	fold := map[rune]string{'ß': "ss", 'Æ': "AE", 'æ': "ae", 'Ø': "O", 'ø': "o", '’': "'", '‘': "'", '“': `"`, '”': `"`, '–': "-", '—': "-"}
	for _, group := range []struct{ to, from string }{
		{"a", "àáâãäå"}, {"A", "ÀÁÂÃÄÅ"}, {"e", "èéêë"}, {"E", "ÈÉÊË"}, {"i", "ìíîï"}, {"I", "ÌÍÎÏ"},
		{"o", "òóôõö"}, {"O", "ÒÓÔÕÖ"}, {"u", "ùúûü"}, {"U", "ÙÚÛÜ"}, {"n", "ñ"}, {"N", "Ñ"}, {"c", "ç"}, {"C", "Ç"}, {"y", "ýÿ"},
	} {
		for _, r := range group.from {
			fold[r] = group.to
		}
	}
	var b strings.Builder
	for _, r := range title {
		switch {
		case r == '\\':
			b.WriteByte('/')
		case r >= 0x20 && r < 0x7F:
			b.WriteRune(r)
		case fold[r] != "":
			b.WriteString(fold[r])
		default:
			b.WriteByte('?')
		}
	}
	text := strings.TrimSpace(b.String())
	if len(text) > 60 {
		text = strings.TrimSpace(text[:60])
	}
	date := "Recorded " + t.Format("2006-01-02 15:04")
	if text == "" {
		return date
	}
	return text + `\` + date
}
