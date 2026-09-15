package scenarioverify

import (
	"encoding/json"
	"io"
	"strings"
	"time"
)

type Event struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test"`
	Elapsed *float64  `json:"Elapsed"`
	Output  string    `json:"Output"`
	Failed  bool      `json:"Failed"`
}

func DecodeEvents(r io.Reader) ([]Event, error) {
	dec := json.NewDecoder(r)
	var events []Event
	for {
		var ev Event
		if err := dec.Decode(&ev); err != nil {
			if err == io.EOF {
				return events, nil
			}
			return nil, err
		}
		ev.Action = strings.ToLower(strings.TrimSpace(ev.Action))
		events = append(events, ev)
	}
}
