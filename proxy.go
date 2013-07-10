package main

import "log"
import "net"

func connect(from net.Conn, to net.Conn) {
    println("Connecting", from, "to", to)
    for {
        buffer := make([]byte, 512)
        read, error := from.Read(buffer)
        if error != nil {
            log.Fatal(error)
            return
        }
        data := buffer[0:read]
        println("Read", data, "from", from)
        _, error = to.Write(data)
        if error != nil {
            println("Error writing", error)
        }
        println("Written", data, "to", to)
        
    }
}

func doProxy(downstream net.Conn) {
    println("In doProxy")

    println("Connecting to downstream")
    upstream, error := net.Dial("tcp", "www.google.com:80")
    if error != nil {
        println("Couldn't connect to upstream", error)
        return
    }
    println("Connected to downstream", downstream, upstream)

    go connect(downstream, upstream)
    go connect(upstream, downstream)
}


func main() {
    listener, error := net.Listen("tcp", "0.0.0.0:80")
    if error != nil {
        println("Couldn't start listening", error)
    }
    println("Started, listening...")
    for {
        connection, error := listener.Accept()
        if error != nil {
            println("Accept error", error)
            return
        }

        println("New connection", connection)
        go doProxy(connection)
    }
}
