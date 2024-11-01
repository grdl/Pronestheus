package nest

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"

	"github.com/go-kit/kit/log"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	errNon200Response      = errors.New("nest API responded with non-200 code")
	errFailedParsingURL    = errors.New("failed parsing OpenWeatherMap API URL")
	errFailedUnmarshalling = errors.New("failed unmarshalling Nest API response body")
	errFailedRequest       = errors.New("failed Nest API request")
	errFailedReadingBody   = errors.New("failed reading Nest API response body")
)

// Thermostat stores thermostat data received from Nest API.
type Thermostat struct {
	ID           string
	Label        string
	AmbientTemp  float64
	SetpointTemp float64
	SetpointTempHvac float64
	Humidity     float64
	Status       string
    Mode         string 
}

// Config provides the configuration necessary to create the Collector.
type Config struct {
	Logger            log.Logger
	Timeout           int
	APIURL            string
	OAuthClientID     string
	OAuthClientSecret string
	RefreshToken      string
	ProjectID         string
	OAuthToken        *oauth2.Token
}

// Collector implements the Collector interface, collecting thermostats data from Nest API.
type Collector struct {
	client  *http.Client
	url     string
	logger  log.Logger
	metrics *Metrics
}

// Metrics contains the metrics collected by the Collector.
type Metrics struct {
	up               *prometheus.Desc
	ambientTemp      *prometheus.Desc
	setpointTemp     *prometheus.Desc
	setpointTempHvac *prometheus.Desc
	humidity         *prometheus.Desc
	heating          *prometheus.Desc
	cooling          *prometheus.Desc
    mode             *prometheus.Desc
    modeOff          *prometheus.Desc
    modeHeat         *prometheus.Desc
    modeCool         *prometheus.Desc
    modeHeatCool     *prometheus.Desc
}

// New creates a Collector using the given Config.
func New(cfg Config) (*Collector, error) {
	if _, err := url.ParseRequestURI(cfg.APIURL); err != nil {
		return nil, errors.Wrap(errFailedParsingURL, err.Error())
	}

	oauthConfig := &oauth2.Config{
		ClientID:     cfg.OAuthClientID,
		ClientSecret: cfg.OAuthClientSecret,
		Scopes:       []string{"https://www.googleapis.com/auth/sdm.service"},
		Endpoint:     endpoints.Google,
	}

	// If token is not provided we create a new one using RefreshToken. Using this token, the client will automatically
	// get, and refresh, a valid access token for the API.
	if cfg.OAuthToken == nil {
		cfg.OAuthToken = &oauth2.Token{
			TokenType:    "Bearer",
			RefreshToken: cfg.RefreshToken,
		}
	}

	client := oauthConfig.Client(context.Background(), cfg.OAuthToken)
	client.Timeout = time.Duration(cfg.Timeout) * time.Millisecond

	collector := &Collector{
		client:  client,
		url:     strings.TrimRight(cfg.APIURL, "/") + "/enterprises/" + cfg.ProjectID + "/devices/",
		logger:  cfg.Logger,
		metrics: buildMetrics(),
	}

	return collector, nil
}

func buildMetrics() *Metrics {
    var nestLabels = []string{"id", "label"}
    return &Metrics{
        up:               prometheus.NewDesc("nest_up", "Was talking to Nest API successful.", nil, nil),
        ambientTemp:      prometheus.NewDesc("nest_ambient_temperature_fahrenheit", "Inside temperature in Fahrenheit.", nestLabels, nil),
        setpointTemp:     prometheus.NewDesc("nest_setpoint_temperature_fahrenheit", "Setpoint temperature in Fahrenheit.", nestLabels, nil),
        setpointTempHvac: prometheus.NewDesc("nest_setpoint_temperature_hvac_fahrenheit", "Setpoint HVAC temperature in Fahrenheit.", nestLabels, nil),
        humidity:         prometheus.NewDesc("nest_humidity_percent", "Inside humidity.", nestLabels, nil),
        heating:          prometheus.NewDesc("nest_heating", "Is thermostat heating.", nestLabels, nil),
        cooling:          prometheus.NewDesc("nest_cooling", "Is thermostat cooling.", nestLabels, nil),
		mode:             prometheus.NewDesc("nest_thermostat_mode", "Current thermostat mode", append(nestLabels, "mode"), nil),    
		modeOff: prometheus.NewDesc("nest_thermostat_mode_off", "Thermostat mode OFF", nestLabels, nil),
		modeHeat: prometheus.NewDesc("nest_thermostat_mode_heat", "Thermostat mode HEAT", nestLabels, nil),
		modeCool: prometheus.NewDesc("nest_thermostat_mode_cool", "Thermostat mode COOL", nestLabels, nil),
		modeHeatCool: prometheus.NewDesc("nest_thermostat_mode_heatcool", "Thermostat mode HEATCOOL", nestLabels, nil),
	}
}  

// Describe implements the prometheus.Describe interface.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.metrics.up
	ch <- c.metrics.ambientTemp
	ch <- c.metrics.setpointTemp
	ch <- c.metrics.setpointTempHvac
	ch <- c.metrics.humidity
	ch <- c.metrics.heating
	ch <- c.metrics.cooling
}

func modeToFloat(mode string) float64 {
    switch mode {
    case "OFF":
        return 0
    case "HEAT":
        return 1
    case "COOL":
        return 2
    case "ECO":
        return 3
    default:
        return -1 // Unknown mode
    }
}

// Collect implements the prometheus.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	thermostats, err := c.getNestReadings()
	if err != nil {
		ch <- prometheus.MustNewConstMetric(c.metrics.up, prometheus.GaugeValue, 0)
		c.logger.Log("level", "error", "message", "Failed collecting Nest data", "stack", errors.WithStack(err))
		return
	}

	c.logger.Log("level", "debug", "message", "Successfully collected Nest data")
	ch <- prometheus.MustNewConstMetric(c.metrics.up, prometheus.GaugeValue, 1)

	for _, therm := range thermostats {
		labels := []string{therm.ID, strings.Replace(therm.Label, " ", "-", -1)}
		
		ch <- prometheus.MustNewConstMetric(c.metrics.ambientTemp, prometheus.GaugeValue, therm.AmbientTemp, labels...)
		ch <- prometheus.MustNewConstMetric(c.metrics.setpointTemp, prometheus.GaugeValue, therm.SetpointTemp, labels...)
		ch <- prometheus.MustNewConstMetric(c.metrics.setpointTempHvac, prometheus.GaugeValue, therm.SetpointTempHvac, labels...)
		ch <- prometheus.MustNewConstMetric(c.metrics.humidity, prometheus.GaugeValue, therm.Humidity, labels...)
		ch <- prometheus.MustNewConstMetric(c.metrics.heating, prometheus.GaugeValue, b2f(therm.Status == "HEATING"), labels...)
		ch <- prometheus.MustNewConstMetric(c.metrics.cooling, prometheus.GaugeValue, b2f(therm.Status == "COOLING"), labels...)

		ch <- prometheus.MustNewConstMetric(c.metrics.modeOff, prometheus.GaugeValue, b2f(therm.Mode == "OFF"), labels...)
		ch <- prometheus.MustNewConstMetric(c.metrics.modeHeat, prometheus.GaugeValue, b2f(therm.Mode == "HEAT"), labels...)
		ch <- prometheus.MustNewConstMetric(c.metrics.modeCool, prometheus.GaugeValue, b2f(therm.Mode == "COOL"), labels...)
		ch <- prometheus.MustNewConstMetric(c.metrics.modeHeatCool, prometheus.GaugeValue, b2f(therm.Mode == "HEATCOOL"), labels...)
		

		// Append mode to labels and send the mode metric
		// labelValues := append(labels, therm.Mode)
		activeMode := modeToFloat(therm.Mode)
		if activeMode >= 0 {
			ch <- prometheus.MustNewConstMetric(c.metrics.mode, prometheus.GaugeValue, 1, append(labels, therm.Mode)...)
		}
	}
}


func (c *Collector) getNestReadings() (thermostats []*Thermostat, err error) {
	res, err := c.client.Get(c.url)
	if err != nil {
		return nil, errors.Wrap(errFailedRequest, err.Error())
	}

	if res.StatusCode != 200 {
		return nil, errors.Wrap(errNon200Response, fmt.Sprintf("code: %d", res.StatusCode))
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, errors.Wrap(errFailedReadingBody, err.Error())
	}

	// Iterate over the array of "devices" returned from the API and unmarshall them into Thermostat objects.
	gjson.Get(string(body), "devices").ForEach(func(_, device gjson.Result) bool {
		// Skip to next device if the current one is not a thermostat.
		if device.Get("type").String() != "sdm.devices.types.THERMOSTAT" {
			return true
		}

		thermostat := Thermostat{
			ID:           device.Get("name").String(),
			Label:        device.Get("traits.sdm\\.devices\\.traits\\.Info.customName").String(),
			AmbientTemp:  device.Get("traits.sdm\\.devices\\.traits\\.Temperature.ambientTemperatureCelsius").Float() * 9/5 + 32,
			SetpointTemp: device.Get("traits.sdm\\.devices\\.traits\\.ThermostatTemperatureSetpoint.heatCelsius").Float() * 9/5 + 32,
			SetpointTempHvac: device.Get("traits.sdm\\.devices\\.traits\\.ThermostatTemperatureSetpoint.coolCelsius").Float() * 9/5 + 32,
			Humidity:     device.Get("traits.sdm\\.devices\\.traits\\.Humidity.ambientHumidityPercent").Float(),
			Status:       device.Get("traits.sdm\\.devices\\.traits\\.ThermostatHvac.status").String(),
			Mode: device.Get("traits.sdm\\.devices\\.traits\\.ThermostatMode.mode").String(),
		}

		thermostats = append(thermostats, &thermostat)
		return true
	})

	if len(thermostats) == 0 {
		return nil, errors.Wrap(errFailedUnmarshalling, "no valid thermostats in devices list")
	}

	return thermostats, nil
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
