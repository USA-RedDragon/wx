package importer

import "github.com/USA-RedDragon/wx/internal/wx"

const (
	staleOutdoor = 30 * 60
	staleIndoor  = 60 * 60
)

func outdoorObs() []string {
	return []string{wx.OutTemp, wx.OutHumidity, wx.WindSpeed, wx.WindGust, wx.WindDir, wx.UV, wx.Dewpoint, wx.HeatIndex, wx.WindChill, wx.Rain}
}

func indoorObs() []string {
	return []string{wx.InTemp, wx.InHumidity}
}

func DropStale(ts []int64, vals []map[string]float64, key string, drop []string, maxSecs int64) int {
	dropped := 0
	i := 0
	for i < len(ts) {
		v, ok := vals[i][key]
		if !ok {
			i++
			continue
		}
		j := i + 1
		for j < len(ts) {
			w, ok := vals[j][key]
			if !ok || w != v {
				break
			}
			j++
		}
		if ts[j-1]-ts[i] > maxSecs {
			for k := i + 1; k < j; k++ {
				for _, d := range drop {
					delete(vals[k], d)
				}
				dropped++
			}
		}
		i = j
	}
	return dropped
}
