package report

import (
	"context"
	"errors"
	"os/user"
	"sort"
	"strings"
	"time"

	"github.com/solworktech/trackz/internal/store"
)

// StatusPresets are the statuses offered before free text.
var StatusPresets = []string{
	"Out to lunch",
	"Meeting",
	"Coffee break",
	"Phone call",
	"Away from desk",
	"Errand",
}

// Status is a period the user marked as away from the computer's normal
// use ("Out to lunch", ...). It is recorded as a trackz record with
// app_name trackz-viz and the status as the window title.
//
// While a status is set, the time counts towards it alone: the tracker
// still records whatever window has focus (typically trackz-viz itself) or
// a screen lock, but reports leave out the part of those records that
// falls within the status.
type Status struct {
	ID    int64
	Text  string
	Start time.Time
}

// ErrStatusActive is returned by StartStatus while another status is set.
var ErrStatusActive = errors.New("a status is already set")

// ErrEmptyStatus is returned by StartStatus for blank text.
var ErrEmptyStatus = errors.New("the status text is empty")

// StartStatus records that the status text starts now.
func (s *Service) StartStatus(ctx context.Context, text string) (Status, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Status{}, ErrEmptyStatus
	}
	if cur, err := s.ActiveStatus(ctx); err != nil {
		return Status{}, err
	} else if cur != nil {
		return Status{}, ErrStatusActive
	}
	owner := ""
	if u, err := user.Current(); err == nil {
		owner = u.Username
	}
	now := s.now()
	id, err := s.st.InsertEvent(ctx, store.Event{
		ProcessName:  store.StatusProcessName,
		AppName:      store.StatusAppName,
		WindowName:   text,
		ProcessOwner: owner,
	}, now.Unix())
	if err != nil {
		return Status{}, err
	}
	return Status{ID: id, Text: text, Start: time.Unix(now.Unix(), 0)}, nil
}

// EndStatus records that st ends now.
func (s *Service) EndStatus(ctx context.Context, st Status) error {
	end := s.now().Unix()
	if end < st.Start.Unix() {
		end = st.Start.Unix() // clock went back
	}
	return s.st.SetFocusEnd(ctx, st.ID, end)
}

// ActiveStatus returns the status that is set, or nil. One left open when
// trackz-viz was killed is still set, so it can be ended.
func (s *Service) ActiveStatus(ctx context.Context) (*Status, error) {
	r, ok, err := s.st.OpenStatus(ctx)
	if err != nil || !ok {
		return nil, err
	}
	return &Status{ID: r.ID, Text: r.WindowName, Start: time.Unix(r.Start, 0)}, nil
}

// clipStatuses removes from every other record the time that falls within
// a status, splitting a record that spans one. Status records are kept
// whole. recs are as returned by store.Records (End set, also for the open
// ones).
func clipStatuses(recs []store.Record) []store.Record {
	var spans [][2]int64
	for _, r := range recs {
		if r.ProcessName == store.StatusProcessName {
			spans = append(spans, [2]int64{r.Start, r.End})
		}
	}
	if len(spans) == 0 {
		return recs
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	out := make([]store.Record, 0, len(recs))
	for _, r := range recs {
		if r.ProcessName == store.StatusProcessName {
			out = append(out, r)
			continue
		}
		start := r.Start
		for _, sp := range spans {
			if sp[1] <= start || sp[0] >= r.End {
				continue
			}
			if sp[0] > start {
				piece := r
				piece.Start, piece.End = start, sp[0]
				out = append(out, piece)
			}
			start = max(start, sp[1])
		}
		if start < r.End {
			piece := r
			piece.Start = start
			out = append(out, piece)
		}
	}
	return out
}
