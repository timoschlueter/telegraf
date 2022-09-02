//go:generate ../../../tools/readme_config_includer/generator

package librelinkup

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"io"
	"net/http"
	"time"
)

// DO NOT REMOVE THE NEXT TWO LINES! This is required to embed the sampleConfig data.
//
//go:embed sample.conf
var sampleConfig string

type LibreLinkUp struct {
	Email     string
	Password  string
	PatientId string
	Region    string
	Version   string
	Product   string
	ApiUrl    string

	client  *http.Client
	session Session
}

func LibreLinkUpInput() *LibreLinkUp {
	tr := &http.Transport{
		ResponseHeaderTimeout: 10 * time.Second,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   15 * time.Second,
	}

	return &LibreLinkUp{
		client:  client,
		Version: "4.2.2",
		Product: "llu.ios"}
}

func (l *LibreLinkUp) Init() error {
	l.ApiUrl = getRegionalApiUrl(l)
	err := login(l.Email, l.Password, l)
	if err != nil {
		return err
	}
	return nil
}

func (l *LibreLinkUp) Gather(acc telegraf.Accumulator) error {
	measurements, err := getGlucoseMeasurements(l)
	if err != nil {
		return err
	}

	tags := map[string]string{
		"patient_id": measurements.Data.Connection.PatientID,
		"sensor_sn":  measurements.Data.Connection.Sensor.Sn,
	}

	fields := make(map[string]interface{})
	fields["mg_dl"] = measurements.Data.Connection.GlucoseMeasurement.ValueInMgPerDl
	fields["mmol_l"] = float32(measurements.Data.Connection.GlucoseMeasurement.ValueInMgPerDl) * 0.0555
	fields["timestamp"] = time.Unix(int64(measurements.Data.Connection.Sensor.A), 0).UTC().String()

	acc.AddFields("librelinkup", fields, tags)
	return nil
}

func (*LibreLinkUp) SampleConfig() string {
	return sampleConfig
}

func init() {
	inputs.Add("librelinkup", func() telegraf.Input {
		return LibreLinkUpInput()
	})
}

// Login and save session to global variable
func login(email string, password string, l *LibreLinkUp) error {
	values := map[string]string{
		"email":    email,
		"password": password}

	session := &Session{}
	err := callApi("/llu/auth/login", "POST", values, false, session, l)
	if err != nil {
		return err
	}
	if session.Status == 0 {
		l.session = *session
		return nil
	} else {
		return errors.New("invalid login credentials. Please check your username/email and password")
	}
}

// Get all the available connections
func getLibreLinkUpConnection(l *LibreLinkUp) (string, error) {
	if l.session.Status == 0 {
		connection := &Connection{}
		err := callApi("/llu/connections", "GET", nil, true, connection, l)
		if err != nil {
			return "", errors.New("error getting connections: " + err.Error())
		}

		if len(connection.Data) == 0 {
			return "", errors.New("no LibreLinkUp connection found")
		}

		if len(connection.Data) == 1 {
			return connection.Data[0].PatientID, nil
		}

		if len(connection.Data) > 1 {
			var patientId string
			var patientInformation string

			for i := range connection.Data {
				patientInformation = fmt.Sprintf("%s - %s: %s %s\n",
					patientInformation,
					connection.Data[i].PatientID,
					connection.Data[i].FirstName,
					connection.Data[i].LastName)
				if connection.Data[i].PatientID == l.PatientId {
					patientId = connection.Data[i].PatientID
				}
			}

			if l.PatientId == "" {
				return "", errors.New("more than one specified patient-id was found:\n" + patientInformation +
					" please set a patient_id in the config")
			}

			if patientId == "" {
				return "", errors.New("the specified patient-id was not found")
			} else {
				return patientId, nil
			}
		}
	} else {
		return "", errors.New("invalid login credentials. Please check your username/email and password")
	}
	return "", errors.New("invalid login credentials. Please check your username/email and password")
}

// Get the glucose measurements
func getGlucoseMeasurements(l *LibreLinkUp) (Measurements, error) {
	var glucoseMeasurements Measurements
	if l.session.Status == 0 {
		patientId, err := getLibreLinkUpConnection(l)
		if err != nil {
			return glucoseMeasurements, errors.New("error getting connections: " + err.Error())
		}
		measurements := &Measurements{}
		err = callApi("/llu/connections/"+patientId+"/graph", "GET", nil, true, measurements, l)
		if err != nil {
			return glucoseMeasurements, errors.New("error getting glucose measurement from the llu backend: " + err.Error())
		}
		glucoseMeasurements = *measurements
		return glucoseMeasurements, nil
	} else {
		return glucoseMeasurements, errors.New("error getting glucose measurement from the llu backend: login status != 0")
	}
}

// Makes the actual REST call to the LLU backend
func callApi(endpoint string, method string, body map[string]string, authenticated bool, target interface{}, l *LibreLinkUp) error {
	req, err := http.NewRequest(method, l.ApiUrl+endpoint, bytes.NewBuffer(createBody(body)))
	if err != nil {
		return err
	}

	addHeaders(*req, authenticated, l)

	if err != nil {
		return err
	}

	response, err := l.client.Do(req)

	if err != nil {
		return err
	}

	defer response.Body.Close()

	if response.StatusCode == 400 {
		reAuthError := login(l.Email, l.Password, l)
		if reAuthError != nil {
			return errors.New(reAuthError.Error())
		}
	}

	bodyBytes, err := io.ReadAll(response.Body)

	if err != nil {
		return err
	}
	return json.Unmarshal(bodyBytes, &target)
}

// Adds the needed header values
func addHeaders(req http.Request, withAuth bool, l *LibreLinkUp) {
	req.Header.Add("User-Agent", "FreeStyle LibreLink Up NightScout Uploader")
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("version", l.Version)
	req.Header.Add("product", l.Product)
	req.Header.Add("Accept-Encoding", "gzip, deflate, br")
	req.Header.Add("Connection", "keep-alive")
	req.Header.Add("Pragma", "no-cache")
	req.Header.Add("Cache-Control", "no-cache")
	if withAuth {
		req.Header.Add("Authorization", "Bearer "+l.session.Data.AuthTicket.Token)
	}
}

// Takes the body values and converts them to the needed byte array
func createBody(values map[string]string) []byte {
	jsonData, err := json.Marshal(values)
	if err != nil {
		_ = fmt.Errorf("error creating body parameters: %s", err.Error())
	}
	return jsonData
}

// Gets the API base url for the region
func getRegionalApiUrl(l *LibreLinkUp) string {
	return "https://" + availableApiUrls()[l.Region]
}

// All known API base urls
func availableApiUrls() map[string]string {
	var apiUrls = make(map[string]string)
	apiUrls["US"] = "api-us.libreview.io"
	apiUrls["EU"] = "api-eu.libreview.io"
	apiUrls["DE"] = "api-de.libreview.io"
	apiUrls["FR"] = "api-fr.libreview.io"
	apiUrls["JP"] = "api-jp.libreview.io"
	apiUrls["AP"] = "api-ap.libreview.io"
	apiUrls["AU"] = "api-au.libreview.io"
	apiUrls["AE"] = "api-ae.libreview.io"
	return apiUrls
}

type DateString struct {
	time.Time
}

func (t *DateString) UnmarshalJSON(b []byte) (err error) {
	date, err := time.Parse(`"1/2/2006 3:04:05 PM"`, string(b))
	if err != nil {
		return err
	}
	t.Time = date
	return
}

type Connection struct {
	Status int `json:"status"`
	Data   []struct {
		ID         string `json:"id"`
		PatientID  string `json:"patientId"`
		Country    string `json:"country"`
		Status     int    `json:"status"`
		FirstName  string `json:"firstName"`
		LastName   string `json:"lastName"`
		TargetLow  int    `json:"targetLow"`
		TargetHigh int    `json:"targetHigh"`
		Uom        int    `json:"uom"`
		Sensor     struct {
			DeviceID string `json:"deviceId"`
			Sn       string `json:"sn"`
			A        int    `json:"a"`
			W        int    `json:"w"`
			Pt       int    `json:"pt"`
			S        bool   `json:"s"`
			Lj       bool   `json:"lj"`
		} `json:"sensor"`
		AlarmRules struct {
			C bool `json:"c"`
			H struct {
				Th   int     `json:"th"`
				Thmm float64 `json:"thmm"`
				D    int     `json:"d"`
				F    float64 `json:"f"`
			} `json:"h"`
			F struct {
				Th   int     `json:"th"`
				Thmm int     `json:"thmm"`
				D    int     `json:"d"`
				Tl   int     `json:"tl"`
				Tlmm float64 `json:"tlmm"`
			} `json:"f"`
			L struct {
				Th   int     `json:"th"`
				Thmm float64 `json:"thmm"`
				D    int     `json:"d"`
				Tl   int     `json:"tl"`
				Tlmm float64 `json:"tlmm"`
			} `json:"l"`
			Nd struct {
				I int `json:"i"`
				R int `json:"r"`
				L int `json:"l"`
			} `json:"nd"`
			P   int `json:"p"`
			R   int `json:"r"`
			Std struct {
			} `json:"std"`
		} `json:"alarmRules"`
		GlucoseMeasurement struct {
			FactoryTimestamp DateString  `json:"FactoryTimestamp"`
			Timestamp        DateString  `json:"Timestamp"`
			Type             int         `json:"type"`
			ValueInMgPerDl   int         `json:"ValueInMgPerDl"`
			TrendArrow       int         `json:"TrendArrow"`
			TrendMessage     interface{} `json:"TrendMessage"`
			MeasurementColor int         `json:"MeasurementColor"`
			GlucoseUnits     int         `json:"GlucoseUnits"`
			Value            int         `json:"Value"`
			IsHigh           bool        `json:"isHigh"`
			IsLow            bool        `json:"isLow"`
		} `json:"glucoseMeasurement"`
		GlucoseItem struct {
			FactoryTimestamp DateString  `json:"FactoryTimestamp"`
			Timestamp        DateString  `json:"Timestamp"`
			Type             int         `json:"type"`
			ValueInMgPerDl   int         `json:"ValueInMgPerDl"`
			TrendArrow       int         `json:"TrendArrow"`
			TrendMessage     interface{} `json:"TrendMessage"`
			MeasurementColor int         `json:"MeasurementColor"`
			GlucoseUnits     int         `json:"GlucoseUnits"`
			Value            int         `json:"Value"`
			IsHigh           bool        `json:"isHigh"`
			IsLow            bool        `json:"isLow"`
		} `json:"glucoseItem"`
		GlucoseAlarm  interface{} `json:"glucoseAlarm"`
		PatientDevice struct {
			Did                 string `json:"did"`
			Dtid                int    `json:"dtid"`
			V                   string `json:"v"`
			Ll                  int    `json:"ll"`
			Hl                  int    `json:"hl"`
			U                   int    `json:"u"`
			FixedLowAlarmValues struct {
				Mgdl  int     `json:"mgdl"`
				Mmoll float64 `json:"mmoll"`
			} `json:"fixedLowAlarmValues"`
			Alarms            bool `json:"alarms"`
			FixedLowThreshold int  `json:"fixedLowThreshold"`
		} `json:"patientDevice,omitempty"`
		Created int `json:"created"`
	} `json:"data"`
	Ticket struct {
		Token    string `json:"token"`
		Expires  int    `json:"expires"`
		Duration int64  `json:"duration"`
	} `json:"ticket"`
}

type Session struct {
	Status int `json:"status"`
	Data   struct {
		User struct {
			ID                    string `json:"id"`
			FirstName             string `json:"firstName"`
			LastName              string `json:"lastName"`
			Email                 string `json:"email"`
			Country               string `json:"country"`
			UILanguage            string `json:"uiLanguage"`
			CommunicationLanguage string `json:"communicationLanguage"`
			AccountType           string `json:"accountType"`
			Uom                   string `json:"uom"`
			DateFormat            string `json:"dateFormat"`
			TimeFormat            string `json:"timeFormat"`
			EmailDay              []int  `json:"emailDay"`
			System                struct {
				Messages struct {
					FirstUsePhoenix                  int    `json:"firstUsePhoenix"`
					FirstUsePhoenixReportsDataMerged int    `json:"firstUsePhoenixReportsDataMerged"`
					LluAnalyticsNewAccount           int    `json:"lluAnalyticsNewAccount"`
					LluGettingStartedBanner          int    `json:"lluGettingStartedBanner"`
					LluNewFeatureModal               int    `json:"lluNewFeatureModal"`
					LvWebPostRelease                 string `json:"lvWebPostRelease"`
				} `json:"messages"`
			} `json:"system"`
			Details struct {
			} `json:"details"`
			TwoFactor struct {
				Code            string `json:"code"`
				Fingerprint     string `json:"fingerprint"`
				PhoneNumber     string `json:"phoneNumber"`
				IsPrimaryMethod bool   `json:"isPrimaryMethod"`
				PrimaryMethod   string `json:"primaryMethod"`
				PrimaryValue    string `json:"primaryValue"`
				SecondaryMethod string `json:"secondaryMethod"`
				SecondaryValue  string `json:"secondaryValue"`
			} `json:"twoFactor"`
			Created   int `json:"created"`
			LastLogin int `json:"lastLogin"`
			Programs  struct {
			} `json:"programs"`
			DateOfBirth int `json:"dateOfBirth"`
			Practices   struct {
			} `json:"practices"`
			Devices struct {
			} `json:"devices"`
			Consents struct {
				RealWorldEvidence struct {
					PolicyAccept int `json:"policyAccept"`
					TouAccept    int `json:"touAccept"`
					History      []struct {
						PolicyAccept int `json:"policyAccept"`
					} `json:"history"`
				} `json:"realWorldEvidence"`
			} `json:"consents"`
		} `json:"user"`
		Messages struct {
			Unread int `json:"unread"`
		} `json:"messages"`
		Notifications struct {
			Unresolved int `json:"unresolved"`
		} `json:"notifications"`
		AuthTicket struct {
			Token    string `json:"token"`
			Expires  int    `json:"expires"`
			Duration int64  `json:"duration"`
		} `json:"authTicket"`
		Invitations interface{} `json:"invitations"`
	} `json:"data"`
}

type Measurements struct {
	Status int `json:"status"`
	Data   struct {
		Connection struct {
			ID         string `json:"id"`
			PatientID  string `json:"patientId"`
			Country    string `json:"country"`
			Status     int    `json:"status"`
			FirstName  string `json:"firstName"`
			LastName   string `json:"lastName"`
			TargetLow  int    `json:"targetLow"`
			TargetHigh int    `json:"targetHigh"`
			Uom        int    `json:"uom"`
			Sensor     struct {
				DeviceID string `json:"deviceId"`
				Sn       string `json:"sn"`
				A        int    `json:"a"`
				W        int    `json:"w"`
				Pt       int    `json:"pt"`
				S        bool   `json:"s"`
				Lj       bool   `json:"lj"`
			} `json:"sensor"`
			AlarmRules struct {
				C bool `json:"c"`
				H struct {
					Th   int     `json:"th"`
					Thmm float64 `json:"thmm"`
					D    int     `json:"d"`
					F    float64 `json:"f"`
				} `json:"h"`
				F struct {
					Th   int     `json:"th"`
					Thmm int     `json:"thmm"`
					D    int     `json:"d"`
					Tl   int     `json:"tl"`
					Tlmm float64 `json:"tlmm"`
				} `json:"f"`
				L struct {
					Th   int     `json:"th"`
					Thmm float64 `json:"thmm"`
					D    int     `json:"d"`
					Tl   int     `json:"tl"`
					Tlmm float64 `json:"tlmm"`
				} `json:"l"`
				Nd struct {
					I int `json:"i"`
					R int `json:"r"`
					L int `json:"l"`
				} `json:"nd"`
				P   int `json:"p"`
				R   int `json:"r"`
				Std struct {
				} `json:"std"`
			} `json:"alarmRules"`
			GlucoseMeasurement struct {
				FactoryTimestamp DateString  `json:"FactoryTimestamp"`
				Timestamp        DateString  `json:"Timestamp"`
				Type             int         `json:"type"`
				ValueInMgPerDl   int         `json:"ValueInMgPerDl"`
				TrendArrow       int         `json:"TrendArrow"`
				TrendMessage     interface{} `json:"TrendMessage"`
				MeasurementColor int         `json:"MeasurementColor"`
				GlucoseUnits     int         `json:"GlucoseUnits"`
				Value            int         `json:"Value"`
				IsHigh           bool        `json:"isHigh"`
				IsLow            bool        `json:"isLow"`
			} `json:"glucoseMeasurement"`
			GlucoseItem struct {
				FactoryTimestamp DateString  `json:"FactoryTimestamp"`
				Timestamp        DateString  `json:"Timestamp"`
				Type             int         `json:"type"`
				ValueInMgPerDl   int         `json:"ValueInMgPerDl"`
				TrendArrow       int         `json:"TrendArrow"`
				TrendMessage     interface{} `json:"TrendMessage"`
				MeasurementColor int         `json:"MeasurementColor"`
				GlucoseUnits     int         `json:"GlucoseUnits"`
				Value            int         `json:"Value"`
				IsHigh           bool        `json:"isHigh"`
				IsLow            bool        `json:"isLow"`
			} `json:"glucoseItem"`
			GlucoseAlarm  interface{} `json:"glucoseAlarm"`
			PatientDevice struct {
				Did                 string `json:"did"`
				Dtid                int    `json:"dtid"`
				V                   string `json:"v"`
				Ll                  int    `json:"ll"`
				Hl                  int    `json:"hl"`
				U                   int    `json:"u"`
				FixedLowAlarmValues struct {
					Mgdl  int     `json:"mgdl"`
					Mmoll float64 `json:"mmoll"`
				} `json:"fixedLowAlarmValues"`
				Alarms            bool `json:"alarms"`
				FixedLowThreshold int  `json:"fixedLowThreshold"`
			} `json:"patientDevice"`
			Created int `json:"created"`
		} `json:"connection"`
		ActiveSensors []struct {
			Sensor struct {
				DeviceID string `json:"deviceId"`
				Sn       string `json:"sn"`
				A        int    `json:"a"`
				W        int    `json:"w"`
				Pt       int    `json:"pt"`
				S        bool   `json:"s"`
				Lj       bool   `json:"lj"`
			} `json:"sensor"`
			Device struct {
				Did                 string `json:"did"`
				Dtid                int    `json:"dtid"`
				V                   string `json:"v"`
				Ll                  int    `json:"ll"`
				Hl                  int    `json:"hl"`
				U                   int    `json:"u"`
				FixedLowAlarmValues struct {
					Mgdl  int     `json:"mgdl"`
					Mmoll float64 `json:"mmoll"`
				} `json:"fixedLowAlarmValues"`
				Alarms            bool `json:"alarms"`
				FixedLowThreshold int  `json:"fixedLowThreshold"`
			} `json:"device"`
		} `json:"activeSensors"`
		GraphData []struct {
			FactoryTimestamp DateString `json:"FactoryTimestamp"`
			Timestamp        DateString `json:"Timestamp"`
			Type             int        `json:"type"`
			ValueInMgPerDl   int        `json:"ValueInMgPerDl"`
			MeasurementColor int        `json:"MeasurementColor"`
			GlucoseUnits     int        `json:"GlucoseUnits"`
			Value            int        `json:"Value"`
			IsHigh           bool       `json:"isHigh"`
			IsLow            bool       `json:"isLow"`
		} `json:"graphData"`
	} `json:"data"`
	Ticket struct {
		Token    string `json:"token"`
		Expires  int    `json:"expires"`
		Duration int64  `json:"duration"`
	} `json:"ticket"`
}
