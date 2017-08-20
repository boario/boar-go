package instrument

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const MaxQueueSize = 10

var requests = make(chan *Request, MaxQueueSize*2)

var appName string
var boarToken string
var boarEnv string
var boarTransmit bool
var boarEndpoint = "https://push.boar.io"

func Start(token string, env string, app string) {
	boarToken = token
	boarEnv = env
	appName = app

	if boarToken != "" {
		boarTransmit = true
		altEndpoint := os.Getenv("BOAR_ENDPOINT")
		if altEndpoint != "" {
			boarEndpoint = altEndpoint
		}
	}

	go processQueue()
}

func processQueue() {
	reqs := make([]*Request, 0, MaxQueueSize*2)
	ticker := time.Tick(5 * time.Second)
	for {
		select {
		case <-ticker:
			go sendQueue(reqs)
			reqs = make([]*Request, 0, MaxQueueSize*2)
		case r := <-requests:
			reqs = append(reqs, r)
			if len(reqs) > MaxQueueSize {
				go sendQueue(reqs)
				reqs = make([]*Request, 0, MaxQueueSize*2)
			}
		}
	}
}

func sendQueue(reqs []*Request) {
	if !boarTransmit || len(reqs) == 0 {
		return
	}
	b, err := json.Marshal(reqs)
	if err != nil {
		log.Printf("Error sending queue: %+v", err)
		return
	}
	cli := http.Client{}
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/requests", boarEndpoint), bytes.NewReader(b))
	req.Header.Add("Authorization", fmt.Sprintf("Token %s", boarToken))
	_, err = cli.Do(req)
	if err != nil {
		log.Printf("Error sending %+v: %+v", req, err)
	}
	// log.Printf("Response: %+v", resp)
	// log.Printf("Queue: %s", string(b))
	return
}

type Request struct {
	RequestID string  `json:"request_id"`
	HopCount  string  `json:"hop_count"`
	Duration  float64 `json:"duration"`
	//Headers   map[string]string `json:"headers"`
	Method    string            `json:"method"`
	Endpoint  string            `json:"endpoint"`
	URI       string            `json:"uri"`
	Hostname  string            `json:"hostname"`
	App       string            `json:"app"`
	StartedAt time.Time         `json:"start"`
	StoppedAt time.Time         `json:"stop"`
	Params    map[string]string `json:"params"`
	Headers   map[string]string `json:"headers"`
	Env       string            `json:"env"`
	Events    Tracer            `json:"events"`
	Status    int               `json:"status"`
}

type Tracer struct {
	StartedAt   time.Time `json:"started_at"`
	StoppedAt   time.Time `json:"stopped_at"`
	Duration    float64   `json:"duration"`
	Tree        []*Tracer `json:"tree"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Parent      *Tracer   `json:"-"`
	lock        sync.Mutex
}

func NewRequest(hostname string, uri string, endpoint string, method string, params url.Values) Request {
	req := Request{
		Endpoint: endpoint,
		Method:   method,
		URI:      uri,
		Hostname: hostname,
		//Headers:  map[string]string{},
		Params:    map[string]string{},
		Headers:   map[string]string{},
		Env:       boarEnv,
		App:       appName,
		StartedAt: time.Now(),
	}

	for k, v := range params {
		req.Params[k] = v[0]
	}

	return req
}

func (r *Request) Stop(root *Tracer, reqid string, hop string, status int, headers http.Header) {
	r.Events = *root
	// for k, v := range c.Request().Header {
	// 	req.Headers[k] = v[0]
	// }
	r.RequestID = reqid
	r.HopCount = hop
	r.Status = status
	reg := regexp.MustCompile("^(X-|Cookie|Authorization|If-)")
	for h, v := range headers {
		if !reg.MatchString(h) {
			r.Headers[h] = v[0]
		}
	}
	r.StoppedAt = time.Now()
	l := float64(r.StoppedAt.Sub(r.StartedAt).Nanoseconds()) / 1000000.0
	parsed := strconv.FormatFloat(l, 'f', 4, 64)
	f, _ := strconv.ParseFloat(parsed, 64)
	r.Duration = f

	requests <- r
}

func NewTracer(parent *Tracer, name string, desc string) (t *Tracer) {
	t = &Tracer{}
	t.Name = name
	t.Description = desc
	t.StartedAt = time.Now()
	t.Parent = parent

	return
}

// instrument
func (t *Tracer) Instrument(name string, desc string, f func(*Tracer)) {
	newT := NewTracer(t, name, desc)
	t.lock.Lock()
	t.Tree = append(t.Tree, newT)
	t.lock.Unlock()
	f(t)
	newT.Stop()
}

func (t *Tracer) Stop() {
	t.StoppedAt = time.Now()
	l := float64(t.StoppedAt.Sub(t.StartedAt).Nanoseconds()) / 1000000.0
	parsed := strconv.FormatFloat(l, 'f', 4, 64)
	f, _ := strconv.ParseFloat(parsed, 64)
	t.Duration = f
	// if t.Parent == nil {
	// 	bt, _ := json.Marshal(t)
	// 	fmt.Printf("Trace: %s", string(bt))
	// }
}
