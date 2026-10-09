package web

import "time"

func (s *Server) SetHeartbeat(d time.Duration) {
	s.heartbeat = d
}
