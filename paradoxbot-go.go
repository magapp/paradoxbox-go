package main

import (
    "fmt"
    "time"
    "bufio"
    "io"
    "os"
    "strings"
    "io/ioutil"
    "encoding/json"
    "net/http"
    "gopkg.in/yaml.v2"
    "github.com/tarm/goserial"
)

type Config struct {
        Port string `yaml:"paradox_port"`
        Baud int `yaml:"paradox_baud"`
        TcpPort int `yaml:"tcp_port"`
        MaxUsers int `yaml:"max_users"`
        MaxAreas int `yaml:"max_areas"`
        MaxZones int `yaml:"max_zones"`
        Webhooks[] struct {
            Event string
            Description string
            Url string
        } `yaml:"webhooks"`
}

type Area struct {
  Name string
  // State can be either of:
  // disarmed,armed,forcearmed,stayarmed,instantarmed
  State string
  ZoneInMemory bool
  Trouble bool
  Active bool
  InProgramming bool
  Alarm bool
  Strobe bool
}

func check(e error) {
  if e != nil {
    panic(e)
  }
}

func httpServe(w http.ResponseWriter, r *http.Request) {
    r.ParseForm() 
    fmt.Println(r.Form)  // print form information in server side
    fmt.Println("path", r.URL.Path)
    fmt.Println("scheme", r.URL.Scheme)
    fmt.Println(r.Form["url_long"])
    for k, v := range r.Form {
        fmt.Println("key:", k)
        fmt.Println("val:", strings.Join(v, ""))
    }
    fmt.Fprintf(w, "Hello astaxie!") // send data to client side
}

func httpStatusArea(w http.ResponseWriter, r *http.Request, areas map[string]Area) {
    w.Header().Set("Content-Type", "application/json")
    jsonString, err := json.Marshal(areas)
    check(err)
    fmt.Fprintf(w, "%s", jsonString)
}

func paradoxWait(s io.ReadWriteCloser, areas *map[string]Area) string {
    ret_data := ""
    reader := bufio.NewReader(s)
    ret_data_bytes, err := reader.ReadBytes('\r')
    check(err)
    ret_data = strings.TrimSpace(string(ret_data_bytes))

    if strings.Contains(ret_data, "fail") || len(ret_data) < 4 {
        fmt.Printf("Got invalid: '%s'\n", ret_data)
        return ""
    }
    // fmt.Printf("Got response: '%s'\n", ret_data)
    paradoxParse(ret_data, areas)
    return ret_data
}

func paradoxSend(s io.ReadWriteCloser, data string) {
    _, err := s.Write([]byte(data + "\r"))
    check(err)
}

func paradoxSendAndWait(s io.ReadWriteCloser, data string, areas *map[string]Area) string {
    retry := 0
    max_retries := 5
    ret_data := ""
    for retry = 0; retry < max_retries; retry++ { 
        paradoxSend(s, data)
        ret_data = paradoxWait(s, areas)
        if len(ret_data) > 0 {
            break
        }
    }
    if retry == (max_retries - 1)  {
        panic("Timeout talking with Paradox")
    }
    return ret_data
}

func paradoxParse(event string, areas *map[string]Area) {
    //fmt.Printf("heja -%s-\n", []byte(event)[0:1])
    //var event = Event{}
    //err := binary.Read([]byte(data), binary.BigEndian, &event)
    //check(err)
    //fmt.Printf("heja -%s-%s-%s-\n", event.Command, event.Group, event.Data)
    //mt.Printf("heja -%s-\n", event[:2])
    //cmd := event[:2]
    //group := event[2:5]
    // fmt.Printf("heja -%s-%s-\n", cmd, group)

    if event[:2] == "AL" { // Area Label
        (*areas)[event[2:5]] = Area{Name: event[5:]}
    }
    if event[:2] == "RA" { // Area status
        // (*areas)[event[2:5]] = Area{Name: event[5:]}
        
    } else {
        fmt.Printf("Got: '%s'\n", event)
    }
}

func main() {
    var paradoxbot_yaml = "/etc/paradoxbot.yaml"

    var config = Config{Baud: 57600, TcpPort: 3001, MaxUsers: 25, MaxAreas: 3, MaxZones: 100}
    var areas = make(map[string]Area)
    

    yamlFile, err := ioutil.ReadFile(paradoxbot_yaml)
    check(err)

    err = yaml.Unmarshal(yamlFile, &config)
    check(err)

    if config.Port == "" {
        panic("Can't find 'paradox_port' in config file")
    }

    fmt.Printf("Connecting to Paradox on '%s' at %d baud...\n", config.Port, config.Baud)

    c := &serial.Config{Name: config.Port, Baud: config.Baud, ReadTimeout: time.Second * 5}
    s, err := serial.OpenPort(c)
    check(err)

    fmt.Printf("Detecting Paradox...\n")
    if !strings.Contains(paradoxSendAndWait(s, "RA001", &areas), "RA001") {
        fmt.Printf("Can't find Paradox on that port, exit.\n")
        os.Exit(-1)
    }

    fmt.Printf("Reading zones...\n")
    for i := 1; i < (config.MaxZones + 1); i++ { 
        paradoxSendAndWait(s, fmt.Sprintf("ZL%03d", i), &areas)
        paradoxSendAndWait(s, fmt.Sprintf("RZ%03d", i), &areas)
    }

    fmt.Printf("Reading areas... ")
    for i := 1; i < (config.MaxAreas + 1); i++ { 
        paradoxSendAndWait(s, fmt.Sprintf("AL%03d", i), &areas)
        paradoxSendAndWait(s, fmt.Sprintf("RA%03d", i), &areas)
    }
 
    for _, area_info := range areas {
	fmt.Printf("'%s'\n", area_info.Name)
    }
    fmt.Printf("\n")

    fmt.Printf("Reading users...\n")
    for i := 1; i < (config.MaxUsers + 1); i++ { 
        paradoxSendAndWait(s, fmt.Sprintf("UL%03d", i), &areas)
    }
 
    s.Close()
    time.Sleep(500 * time.Millisecond)
    // Reopoen without ReadTimeout:
    c = &serial.Config{Name: config.Port, Baud: config.Baud}
    s, err = serial.OpenPort(c)
    check(err)

    // parse all events
    go func() {
        for {
            time.Sleep(25 * time.Millisecond)
            paradoxWait(s, &areas)
            // if len(ret_data) > 0 {
                // fmt.Printf("Got: '%s'\n", ret_data)
            // }
        }
    }()

    // periodically, get area status 
    go func() {
        for {
            for i := 1; i < (config.MaxAreas + 1); i++ { 
                paradoxSend(s, fmt.Sprintf("RA%03d", i))
                time.Sleep(3 * time.Second)
            }
        }
    }()

    fmt.Printf("Listening to TCP port %d...\n", config.TcpPort)

    http.HandleFunc("/", httpServe)
    http.HandleFunc("/status-area", func(w http.ResponseWriter, r *http.Request) {
        httpStatusArea(w, r, areas)
    })
    err = http.ListenAndServe(fmt.Sprint(":", config.TcpPort), nil)
    check(err)

}

