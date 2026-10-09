# wx

[![coverage](https://raw.githubusercontent.com/USA-RedDragon/wx/main/.github/badges/coverage.svg)](https://github.com/USA-RedDragon/wx/actions)

A lightweight replacement for weewx. It subscribes to the station packets that [mqtt-wx](https://github.com/USA-RedDragon/mqtt-wx) publishes, keeps a weewx-style archive and daily summaries in SQLite, and serves the weewx Seasons skin's pages, rendering plots on request instead of regenerating static files on a timer.

## Pages

`index.html`, `statistics.html`, `telemetry.html`, `celestial.html` and `tabular.html` follow the weewx Seasons skin, and `NOAA/NOAA-YYYY-MM.txt` and `NOAA/NOAA-YYYY.txt` are the monthly and yearly climatological summaries. Plots are SVGs named like the Seasons PNGs (`daytempdew.svg`, `yearrain.svg`, `daywindrose.svg`). Pages and plots are rendered when requested and cached until new data arrives, so an idle station costs no CPU.

## Pressure

mqtt-wx publishes station pressure. wx stores it as `pressure` and derives the same values weewx does from `station.altitude-feet`:

- `altimeter`: the ASOS altimeter setting, `(P^0.1903 + 1.313e-5 * elevation_ft)^5.255`
- `barometer`: sea level pressure, `P / exp(-elevation_m / (T_K * 29.263))`, using the current outside temperature. When the outside temperature has been missing for more than six hours, the altimeter setting is used instead

## Importing history

`wx import` loads weewx archives (SQLite), Prometheus series of Home Assistant weather sensors and Home Assistant long-term statistics, in that order of priority. `--wx` merges another wx database, such as one prepared on another machine. Lower-priority sources only fill archive intervals that are still empty, every record keeps the source it came from, and the daily summaries of the affected days are rebuilt afterwards. It is safe to run while the server is running.

```sh
wx import --weewx=weewx.sdb
wx import --wx=seed.db
HA_TOKEN=... wx import --prometheus-url=http://thanos:9090 --homeassistant-url=https://homeassistant.example --since=2026-01-01
```

The Home Assistant long-lived access token is read from the `HA_TOKEN` environment variable.

## Configuration

Configuration is read from `config.yaml` in the working directory (or the file passed with `--config`), environment variables, and command-line flags. Copy [config.example.yaml](config.example.yaml) to `config.yaml` to get started.

<!-- configulator:begin -->

| Key                        | Type    | Default                 | Environment                | Flag                         | Description                                                                                      |
|----------------------------|---------|-------------------------|----------------------------|------------------------------|--------------------------------------------------------------------------------------------------|
| `log-level`                | string  | `info`                  | `LOG_LEVEL`                | `--log-level`                | Logging level for the application. One of debug, info, warn, or error                            |
| `http.bind`                | string  | `[::]`                  | `HTTP_BIND`                | `--http.bind`                | Address to listen on. The default, [::], listens on all interfaces                               |
| `http.port`                | integer | `8080`                  | `HTTP_PORT`                | `--http.port`                | Port to listen on                                                                                |
| `mqtt.broker`              | string  | `mqtt://localhost:1883` | `MQTT_BROKER`              | `--mqtt.broker`              | MQTT broker URL                                                                                  |
| `mqtt.username`            | string  |                         | `MQTT_USERNAME`            | `--mqtt.username`            | MQTT username                                                                                    |
| `mqtt.password`            | string  |                         | `MQTT_PASSWORD`            | `--mqtt.password`            | MQTT password (secret)                                                                           |
| `mqtt.client-id`           | string  | `wx`                    | `MQTT_CLIENT_ID`           | `--mqtt.client-id`           | MQTT client ID                                                                                   |
| `mqtt.topic`               | string  | `weather/station`       | `MQTT_TOPIC`               | `--mqtt.topic`               | Topic carrying mqtt-wx station packets in weewx METRICWX units                                   |
| `mqtt.rain-topic`          | string  |                         | `MQTT_RAIN_TOPIC`          | `--mqtt.rain-topic`          | Optional rtl_433 topic whose rain_mm counter is used for rain instead of the station packet      |
| `station.location`         | string  |                         | `STATION_LOCATION`         | `--station.location`         | Station name shown in the title bar (required)                                                   |
| `station.latitude`         | number  |                         | `STATION_LATITUDE`         | `--station.latitude`         | Station latitude in degrees, north positive (required)                                           |
| `station.longitude`        | number  |                         | `STATION_LONGITUDE`        | `--station.longitude`        | Station longitude in degrees, east positive (required)                                           |
| `station.altitude-feet`    | number  |                         | `STATION_ALTITUDE_FEET`    | `--station.altitude-feet`    | Altitude of the pressure sensor in feet, used to reduce station pressure to sea level (required) |
| `station.timezone`         | string  | `UTC`                   | `STATION_TIMEZONE`         | `--station.timezone`         | Timezone used for day boundaries and display                                                     |
| `station.rain-year-start`  | integer | `1`                     | `STATION_RAIN_YEAR_START`  | `--station.rain-year-start`  | Month the rain year starts in                                                                    |
| `station.week-start`       | integer | `6`                     | `STATION_WEEK_START`       | `--station.week-start`       | Day the week starts on, 0 is Monday and 6 is Sunday                                              |
| `station.archive-interval` | integer | `60`                    | `STATION_ARCHIVE_INTERVAL` | `--station.archive-interval` | Archive record interval in seconds                                                               |
| `station.radar-image`      | string  |                         | `STATION_RADAR_IMAGE`      | `--station.radar-image`      | Radar image URL                                                                                  |
| `station.radar-url`        | string  |                         | `STATION_RADAR_URL`        | `--station.radar-url`        | Radar link URL                                                                                   |
| `station.satellite-image`  | string  |                         | `STATION_SATELLITE_IMAGE`  | `--station.satellite-image`  | Satellite image URL                                                                              |
| `station.satellite-url`    | string  |                         | `STATION_SATELLITE_URL`    | `--station.satellite-url`    | Satellite link URL                                                                               |
| `database.path`            | string  | `wx.db`                 | `DATABASE_PATH`            | `--database.path`            | Path to the SQLite archive database                                                              |
| `nws.enabled`              | boolean | `false`                 | `NWS_ENABLED`              | `--nws.enabled`              | Fetch NWS forecasts and alerts                                                                   |
| `nws.user-agent`           | string  |                         | `NWS_USER_AGENT`           | `--nws.user-agent`           | User-Agent sent to api.weather.gov, which asks for a contact such as (site, email)               |
| `nws.interval`             | string  | `30m`                   | `NWS_INTERVAL`             | `--nws.interval`             | How often to refresh the forecast                                                                |

<!-- configulator:end -->

## License

The page templates, stylesheet and script are ports of the weewx Seasons skin, so wx is licensed under the GPL-3.0, like weewx.
