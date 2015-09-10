package main

import (
    "fmt"
    "time"
    "bufio"
    "io"
    "os"
    "strings"
    "io/ioutil"
    
    "gopkg.in/yaml.v2"
    "github.com/tarm/goserial"
    "net/http"

)

type Config struct {
        Port string `yaml:"paradox_port"`
        Baud int `yaml:"paradox_baud"`
        TcpPort int `yaml:"tcp_port"`
        Webhooks[] struct {
            Event string
            Description string
            Url string
        } `yaml:"webhooks"`
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

func httpStatus(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, "STATUS!") // send data to client side
}

func paradoxWait(s io.ReadWriteCloser) string {
    ret_data := ""
    reader := bufio.NewReader(s)
    ret_data_bytes, err := reader.ReadBytes('\r')
    check(err)
    ret_data = strings.TrimSpace(string(ret_data_bytes))

    if strings.Contains(ret_data, "fail") {
        return ""
    }
    if len(ret_data) < 4 {
        return ""
    }
    // fmt.Printf("Got response: '%s'\n", ret_data)
    return ret_data
}

func paradoxSendWait(s io.ReadWriteCloser, data string) string {
    retry := 0
    max_retries := 5
    ret_data := ""
    for retry = 0; retry < max_retries; retry++ { 
        _, err := s.Write([]byte(data + "\r"))
        check(err)
        ret_data = paradoxWait(s)
        if len(ret_data) > 0 {
            break
        }
    }
    if retry == (max_retries - 1)  {
        panic("Timeout talking with Paradox")
    }
    return ret_data
}

func main() {
    var paradoxbot_yaml = "/etc/paradoxbot.yaml"

    var config = Config{Baud: 57600, TcpPort: 3001}

    yamlFile, err := ioutil.ReadFile(paradoxbot_yaml)
    check(err)

    err = yaml.Unmarshal(yamlFile, &config)
    check(err)

    if config.Port == "" {
        panic("Can't find 'paradox_port' in config file")
    }

    fmt.Printf("Connecting to Paradox on '%s' at %d baud...\n", config.Port, config.Baud)

    // c := &serial.Config{Name: config.Port, Baud: config.Baud, ReadTimeout: time.Second * 5}
    c := &serial.Config{Name: config.Port, Baud: config.Baud}
    s, err := serial.OpenPort(c)
    check(err)

    fmt.Printf("Detecting Paradox...\n")
    if !strings.Contains(paradoxSendWait(s, "RA001"), "RA001") {
        fmt.Printf("Can't find Paradox on that port, exit.\n")
        os.Exit(-1)
    }

    fmt.Printf("Reading zones...\n")
    for i := 1; i < 193; i++ { 
        paradoxSendWait(s, fmt.Sprintf("ZL%03d", i))
        paradoxSendWait(s, fmt.Sprintf("RZ%03d", i))
    }

    fmt.Printf("Reading areas...\n")
    for i := 1; i < 9; i++ { 
        paradoxSendWait(s, fmt.Sprintf("AL%03d", i))
        paradoxSendWait(s, fmt.Sprintf("RA%03d", i))
    }

    fmt.Printf("Reading users...\n")
    for i := 1; i < 100; i++ { 
        paradoxSendWait(s, fmt.Sprintf("UL%03d", i))
    }

    go func() {
        for {
            time.Sleep(25 * time.Millisecond)
            ret_data := paradoxWait(s)
            if len(ret_data) > 0 {
                fmt.Printf("Got: '%s'\n", ret_data)
            }
        }
    }()

    fmt.Printf("Listening to TCP port %d...\n", config.TcpPort)

    http.HandleFunc("/", httpServe)
    http.HandleFunc("/status", httpStatus)
    err = http.ListenAndServe(fmt.Sprint(":", config.TcpPort), nil)
    check(err)

}

