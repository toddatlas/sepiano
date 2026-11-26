package api

import (
	"net/http"
	"strings"

	"sepiano/library"
)

// Recording details: what a recording made on this piano is, for the
// library's catalogue (docs/RECORDING-EDITOR.md, "Recording details"). They
// are given when the recorder is armed, can be changed while it waits or
// records, and go onto the new piece when it is saved. Everything is
// optional: the pianist defaults to the Settings' default pianist, the
// title to the date.

// Recordings made here are catalogued as this piano's own.
const (
	RecordedCollection = "Recorded on this piano"
	RecordedCapturedOn = "This piano (1909 Steinway D, Stahnke SE)"
	RecordedLicence    = "Recorded on this piano by its owner: private"
)

type recordingDetails struct {
	Title     string `json:"title"`
	Composer  string `json:"composer"`
	Performer string `json:"performer"` // the pianist; empty: the default pianist
	Album     string `json:"album"`
	Notes     string `json:"notes"`
	Favorite  bool   `json:"favorite"`
}

func (d recordingDetails) trimmed() recordingDetails {
	d.Title, d.Composer, d.Performer = strings.TrimSpace(d.Title), strings.TrimSpace(d.Composer), strings.TrimSpace(d.Performer)
	d.Album, d.Notes = strings.TrimSpace(d.Album), strings.TrimSpace(d.Notes)
	return d
}

// recDetails returns the details for the recording being made.
func (s *Server) recDetails() recordingDetails {
	s.recMu.Lock()
	defer s.recMu.Unlock()
	return s.rec
}

func (s *Server) setRecDetails(d recordingDetails) {
	s.recMu.Lock()
	s.rec = d.trimmed()
	s.recMu.Unlock()
}

func (s *Server) getRecordingDetails(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.recDetails())
}

// putRecordingDetails changes the details while the recorder waits or
// records; they apply when it saves.
func (s *Server) putRecordingDetails(w http.ResponseWriter, r *http.Request) {
	var d recordingDetails
	if err := readJSON(r, &d); err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.setRecDetails(d)
	writeJSON(w, 200, s.recDetails())
}

// SaveRecording adds a recording the piano has just made to the library,
// with the details given (the deck calls it, through Config.OnRecorded).
func (s *Server) SaveRecording(dir, name, title string) (string, error) {
	s.recMu.Lock()
	d := s.rec
	s.rec = recordingDetails{}
	s.recMu.Unlock()
	if d.Title != "" {
		title = d.Title
	}
	p, err := s.Lib.AddRecording(dir, name, title)
	if err != nil {
		return "", err
	}
	set := s.Lib.Settings()
	patch := library.Patch{}
	str := func(v string) *string { return &v }
	if d.Composer != "" {
		patch.Composer = str(d.Composer)
	}
	performer := d.Performer
	if performer == "" {
		performer = set.RecordingPerformer
	}
	if performer != "" {
		patch.Performer = str(performer)
	}
	if d.Notes != "" {
		patch.Notes = str(d.Notes)
	}
	if d.Album != "" {
		patch.Album = str(d.Album)
		track := 1
		if a, err := s.Lib.Album(d.Album); err == nil {
			for _, t := range a.Tracks {
				if t.Piece.ID != p.ID {
					track = max(track, t.Track+1)
				}
			}
		}
		patch.Track = &track
	}
	inst := set.PreferredInstrument
	if inst == "" {
		inst = library.DefaultInstrument
	}
	patch.Instrument = str(inst)
	patch.Collection, patch.CapturedOn, patch.Licence = str(RecordedCollection), str(RecordedCapturedOn), str(RecordedLicence)
	if d.Favorite {
		fav := true
		patch.Favorite = &fav
	}
	if _, err := s.Lib.Update(p.ID, patch); err != nil {
		return p.ID, err
	}
	return p.ID, nil
}
