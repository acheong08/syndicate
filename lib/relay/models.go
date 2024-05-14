package relay

import (
	"slices"
	"time"
)

type Relays struct {
	Relays []Relay `json:"relays"`
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

func (rs *Relays) Sort(isGreater func(Relay, Relay) bool) {
	slices.SortFunc(rs.Relays, func(a, b Relay) int {
		if isGreater(a, b) {
			return 1
		}
		return -1
	})
}

type Relay struct {
	URL            string   `json:"url"`
	Location       location `json:"location"`
	Stats          stats    `json:"stats"`
	StatsRetrieved string   `json:"stats_retrieved"`
}

func (r *Relay) ParseStatsRetrieved() time.Time {
	return parseDateTime(r.StatsRetrieved)
}

type location struct {
	Country   string  `json:"country"`
	Continent string  `json:"continent"`
	City      string  `json:"city"`
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
}

type stats struct {
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

func (s *stats) ParseStartTime() time.Time {
	// 2024-05-03T18:37:39.913471641+10:00
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
