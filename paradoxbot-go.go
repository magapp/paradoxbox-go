package main

import (
    "fmt"
    "time"
    "github.com/vaughan0/go-ini"
)

func main() {
    var paradoxbot_ini = "/etc/paradoxbot.ini"

    file, err := ini.LoadFile(paradoxbot_ini)
    if err != nil {
        panic(err)
    }

    paradox_port, ok := file.Get("DEFAULT", "paradox_port")
    if !ok {
        panic("Can't find 'paradoxbot_port' in section DEFAULT.\n")
    }

    paradox_baud, ok := file.Get("DEFAULT", "paradox_baud")
    if !ok {
        panic("Can't find 'paradoxbot_baud' in section DEFAULT.\n")
    }

    url_base, ok := file.Get("DEFAULT", "url_base")
    if !ok {
        panic("Can't find 'url_base' in section DEFAULT.\n")
    }
 
    _ = paradox_baud
    _ = url_base
    fmt.Printf("Connecting to Paradox on '%s'...\n", paradox_port)



    fmt.Printf("hello, world\n")
    fmt.Println("The time is", time.Now())
}

