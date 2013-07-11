package main

import "bufio"
import "container/list"
import "io"
import "net"
import "strings"

func doHTTPProxy(downstream net.Conn) {
    println("In doHTTPProxy")

    println("Reading headers")
    reader := bufio.NewReader(downstream)
    hostname := ""
    readLines := list.New()
    for hostname == "" {
        bytes, _, error := reader.ReadLine()
        if error != nil {
            println("Error reading")
            return
        }
        line := string(bytes)
        println("Read ", line)
        readLines.PushBack(line)
        if strings.HasPrefix(line, "Host: ") {
            hostname = strings.TrimPrefix(line, "Host: ")
            println("Found host header:", hostname)
            break
        }
    }
    if hostname == "" {
        println("No host!")
        return
    }

    println("Connecting to downstream")
    upstream, error := net.Dial("tcp", hostname + ":80")
    if error != nil {
        println("Couldn't connect to upstream", error)
        return
    }
    println("Connected to downstream", downstream, upstream)

    println("Replaying headers we've already received")
    for element := readLines.Front(); element != nil; element = element.Next() {
        line := element.Value.(string)
        upstream.Write([]byte(line))
        upstream.Write([]byte("\n"))
        println("Sent ", line)
    }
    println("Replayed headers, connecting for full proxying")

    go io.Copy(upstream, reader)
    go io.Copy(downstream, upstream)
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
        go doHTTPProxy(connection)
    }
}
