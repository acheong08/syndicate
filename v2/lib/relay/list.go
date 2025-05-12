package relay

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/rotisserie/eris"
)

type Relays struct {
	Relays []Relay `json:"relays"`
}

func (rs Relays) ToSlice() []string {
	s := make([]string, len(rs.Relays))
	for i := range rs.Relays {
		s[i] = rs.Relays[i].URL
	}
	return s
}

func (rs Relays) First() Relay {
	return rs.Relays[0]
}

func (rs *Relays) Filter(f func(Relay) bool) {
	var relays []Relay
	for _, r := range rs.Relays {
		if f(r) {
			relays = append(relays, r)
		}
	}
	rs.Relays = relays
}

func (rs *Relays) Sort(isGreater func(Relay, Relay) bool, desc bool) {
	slices.SortFunc(rs.Relays, func(a, b Relay) int {
		if isGreater(a, b) {
			if desc {
				return 1
			}
			return -1
		}
		if desc {
			return -1
		}
		return 1
	})
}

type Relay struct {
	URL            string   `json:"url"`
	Location       Location `json:"location"`
	Stats          Stats    `json:"stats"`
	StatsRetrieved string   `json:"stats_retrieved"`
}

func (r *Relay) ParseStatsRetrieved() time.Time {
	return parseDateTime(r.StatsRetrieved)
}

type Location struct {
	Country   string  `json:"country"`
	Continent string  `json:"continent"`
	City      string  `json:"city"`
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
}

type Stats struct {
	StartTime             string  `json:"startTime"`
	UptimeSeconds         int     `json:"uptimeSeconds"`
	NumPendingSessionKeys int     `json:"numPendingSessionKeys"`
	NumActiveSessions     int     `json:"numActiveSessions"`
	NumConnections        int     `json:"numConnections"`
	NumProxies            int     `json:"numProxies"`
	BytesProxied          int64   `json:"bytesProxied"`
	GoVersion             string  `json:"goVersion"`
	GoOS                  string  `json:"goOS"`
	GoArch                string  `json:"goArch"`
	GoMaxProcs            int     `json:"goMaxProcs"`
	GoNumRoutine          int     `json:"goNumRoutine"`
	Options               options `json:"options"`
}

func (s *Stats) ParseStartTime() time.Time {
	return parseDateTime(s.StartTime)
}

type options struct {
	NetworkTimeout int `json:"network-timeout"`
	PingInterval   int `json:"ping-interval"`
	MessageTimeout int `json:"message-timeout"`
	PerSessionRate int `json:"per-session-rate"`
	GlobalRate     int `json:"global-rate"`
}

func parseDateTime(s string) time.Time {
	// 2024-05-03T18:37:39.913471641+10:00
	t, _ := time.Parse("2006-01-02T15:04:05.999999999-07:00", s)
	return t
}

func List() (r Relays, err error) {
	resp, err := http.Get("https://relays.syncthing.net/endpoint/full")
	if err != nil {
		return r, eris.Wrap(err, "failed to fetch relays endpoint")
	}
	defer resp.Body.Close()

	var relays Relays
	err = json.NewDecoder(resp.Body).Decode(&relays)
	if err != nil {
		return r, eris.Wrap(err, "Could not decode relays as JSON")
	}

	return relays, nil
}
func FindOptimal(ctx context.Context, country string, maxResults int) (r Relays, err error) {
	if maxResults < 1 {
		panic("must have more than 1 result")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	relays, err := List()
	if err != nil {
		return r, eris.Wrap(err, "failed to fetch relay list")
	}
	relays.Filter(func(r Relay) bool {
		if country == "" {
			return true
		}
		return r.Location.Country == country
	})
	log.Printf("Found %d relays", len(relays.Relays))
	relays.Sort(func(a, b Relay) bool {
		// Use a heuristic to determine the best relay
		var aScore, bScore int
		if a.Stats.NumActiveSessions > b.Stats.NumActiveSessions {
			aScore += 1
		} else {
			// We don't add if they are equal
			bScore += btoi(!(a.Stats.NumActiveSessions == b.Stats.NumActiveSessions))
		}
		if a.Stats.UptimeSeconds > b.Stats.UptimeSeconds {
			aScore++
		} else {
			bScore += btoi(!(a.Stats.UptimeSeconds == b.Stats.UptimeSeconds))
		}
		aRate := minButNotZero(a.Stats.Options.GlobalRate, a.Stats.Options.PerSessionRate)
		bRate := minButNotZero(b.Stats.Options.GlobalRate, b.Stats.Options.PerSessionRate)
		if aRate > bRate {
			aScore++
		} else {
			bScore += btoi(!(aRate == bRate))
		}

		return aScore > bScore
	}, true)

	results := make(chan Relay, maxResults)
	var wg sync.WaitGroup
	maxRelays := maxResults
	for i, relay := range relays.Relays {
		if i >= maxResults*2 {
			break
		}
		wg.Add(1)
		go func(relay Relay) {
			defer wg.Done()
			relayURL, _ := url.Parse(relay.URL)
			timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			dialer := &net.Dialer{}
			conn, err := dialer.DialContext(timeoutCtx, "tcp", relayURL.Host)
			if err == nil && conn != nil {
				conn.Close()
				select {
				case results <- relay:
				case <-ctx.Done():
				}
			}
		}(relay)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	relays.Relays = make([]Relay, 0)
	for r := range results {
		relays.Relays = append(relays.Relays, r)
		if len(relays.Relays) >= maxRelays {
			cancel()
			break
		}
	}
	if len(relays.Relays) > 0 {
		return relays, nil
	}
	return r, eris.New("No viable relays found")
}
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
func minButNotZero(a, b int) int {
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}
