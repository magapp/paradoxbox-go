package main

import (
    "fmt"
    "time"
    "bufio"
    "io"
    "os"
    "regexp"
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
        Debug bool `yaml:"debug"`
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
  InAlarm bool
}

type User struct {
  Name string
}

type Zone struct {
  Name string
  // State can be either of:
  // open,ok,tamper,fireloop
  State string
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

func httpStatusUser(w http.ResponseWriter, r *http.Request, users map[string]User) {
    w.Header().Set("Content-Type", "application/json")
    jsonString, err := json.Marshal(users)
    check(err)
    fmt.Fprintf(w, "%s", jsonString)
}

func httpStatusZone(w http.ResponseWriter, r *http.Request, zones map[string]Zone) {
    w.Header().Set("Content-Type", "application/json")
    jsonString, err := json.Marshal(zones)
    check(err)
    fmt.Fprintf(w, "%s", jsonString)
}

func paradoxWait(config Config, s io.ReadWriteCloser, areas *map[string]Area, users *map[string]User, zones *map[string]Zone) string {
    ret_data := ""
    reader := bufio.NewReader(s)
    ret_data_bytes, err := reader.ReadBytes('\r')
    check(err)
    ret_data = strings.TrimSpace(string(ret_data_bytes))

    if strings.Contains(ret_data, "fail") || len(ret_data) < 4 {
        if config.Debug {
            fmt.Printf("Got invalid: '%s'\n", ret_data)
        }
        return ""
    }
    paradoxParse(config, ret_data, areas, users, zones)
    return ret_data
}

func paradoxSend(s io.ReadWriteCloser, data string) {
    _, err := s.Write([]byte(data + "\r"))
    check(err)
}

func paradoxSendAndWait(config Config, s io.ReadWriteCloser, data string, areas *map[string]Area, users *map[string]User, zones *map[string]Zone) string {
    retry := 0
    max_retries := 5
    ret_data := ""
    for retry = 0; retry < max_retries; retry++ { 
        paradoxSend(s, data)
        ret_data = paradoxWait(config, s, areas, users, zones)
        if len(ret_data) > 0 {
            break
        }
    }
    if retry == (max_retries - 1)  {
        panic("Timeout talking with Paradox")
    }
    return ret_data
}

func emitEvent(config Config, event string, name string, label string) {
    if config.Debug {
        fmt.Printf("EmitEvent: event: '%s' name: '%s', label '%s'\n", event, name, label)
    }
    for _,webhook := range config.Webhooks {
        match, _ := regexp.MatchString(webhook.Event, event)
        if match {
            fmt.Printf("Match: '%s'\n", webhook.Description)
        }
    }
}

func paradoxParse(config Config, event string, areas *map[string]Area, users *map[string]User, zones *map[string]Zone){
    if event[:2] == "AL" { // Area Label
        (*areas)[event[2:5]] = Area{Name: event[5:]}
    } else if event[:2] == "RA" { // Area status
        area := (*areas)[event[2:5]]
        state := "" 
        inAlarm := true
        switch event[5] {
            case 'D':
                state = "disarmed"
            case 'A':
                state = "armed"
            case 'F':
                state = "forcearmed"
            case 'S':
                state = "stayarmed"
            case 'I':
                state = "instantarmed"
        }
        if state != area.State {
            area.State = state
            emitEvent(config, event, area.State, area.Name)
        }
        switch event[10] {
            case 'A':
                inAlarm = true
            case 'O':
                inAlarm = false
        }
        if inAlarm != area.InAlarm {
            area.InAlarm = inAlarm
            if area.InAlarm {
                emitEvent(config, event, "alarm", area.Name)
            } else {
                emitEvent(config, event, "ok", area.Name)
            }
        }
        (*areas)[event[2:5]] = area

    } else if event[:2] == "UL" && event[5:] != fmt.Sprintf("User %s", event[2:5]) { // User label
        (*users)[event[2:5]] = User{Name: event[5:]}

    } else if event[:2] == "ZL" && event[5:] != fmt.Sprintf("Zone %s", event[2:5]) { // Zone Label
        (*zones)[event[2:5]] = Zone{Name: event[5:]}

    } else if event[:2] == "RZ" && len((*zones)[event[2:5]].Name) > 0 {
        // only zones with a name are valid
        zone := (*zones)[event[2:5]]
        state := "" 
        switch event[5] {
            case 'O':
                state = "open"
            case 'C':
                state = "ok"
            case 'T':
                state = "tamper"
            case 'F':
                state = "fireloop"
        }
        if state != zone.State {
            zone.State = state
            emitEvent(config, event, zone.State, zone.Name)
        }
        (*zones)[event[2:5]] = zone
    } else if string(event[0]) == "G" && event[1:4] == "000" {
        // event OK
        zone := (*zones)[event[5:8]]
        zone.State = "ok"
        (*zones)[event[5:8]] = zone
        emitEvent(config, event, zone.State, zone.Name)
    } else if string(event[0]) == "G" && event[1:4] == "001" {
        // event Open
        zone := (*zones)[event[5:8]]
        zone.State = "open"
        (*zones)[event[5:8]] = zone
        emitEvent(config, event, zone.State, zone.Name)
    } else if string(event[0]) == "G" && event[1:4] == "002" {
        // event Tamper
        zone := (*zones)[event[5:8]]
        zone.State = "tamper"
        (*zones)[event[5:8]] = zone
        emitEvent(config, event, zone.State, zone.Name)
    } else {
        if config.Debug {
            fmt.Printf("Got unknown: '%s'\n", event)
        }
    }
}

func main() {
    var paradoxbot_yaml = "/etc/paradoxbot.yaml"

    config := Config{Baud: 57600, TcpPort: 3001, MaxUsers: 25, MaxAreas: 3, MaxZones: 100, Debug: false}
    var areas = make(map[string]Area)
    var users = make(map[string]User)
    var zones = make(map[string]Zone)
    

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
    if !strings.Contains(paradoxSendAndWait(config, s, "RA001", &areas, &users, &zones), "RA001") {
        fmt.Printf("Can't find Paradox on that port, exit.\n")
        os.Exit(-1)
    }

    fmt.Printf("Reading zones...\n")
    for i := 1; i < (config.MaxZones + 1); i++ { 
        paradoxSendAndWait(config, s, fmt.Sprintf("ZL%03d", i), &areas, &users, &zones)
        paradoxSendAndWait(config, s, fmt.Sprintf("RZ%03d", i), &areas, &users, &zones)
    }

    fmt.Printf("Reading areas... ")
    for i := 1; i < (config.MaxAreas + 1); i++ { 
        paradoxSendAndWait(config, s, fmt.Sprintf("AL%03d", i), &areas, &users, &zones)
        paradoxSendAndWait(config, s, fmt.Sprintf("RA%03d", i), &areas, &users, &zones)
    }
 
    for _, area_info := range areas {
	fmt.Printf("'%s'\n", area_info.Name)
    }
    fmt.Printf("\n")

    fmt.Printf("Reading users...\n")
    for i := 1; i < (config.MaxUsers + 1); i++ { 
        paradoxSendAndWait(config, s, fmt.Sprintf("UL%03d", i), &areas, &users, &zones)
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
            paradoxWait(config, s, &areas, &users, &zones)
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
    http.HandleFunc("/status-user", func(w http.ResponseWriter, r *http.Request) {
        httpStatusUser(w, r, users)
    })
    http.HandleFunc("/status-zone", func(w http.ResponseWriter, r *http.Request) {
        httpStatusZone(w, r, zones)
    })
    err = http.ListenAndServe(fmt.Sprint(":", config.TcpPort), nil)
    check(err)

}

