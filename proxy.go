package main

import (
    "bufio"
    "container/list"
    "io"
    "net"
    "strconv"
    "strings"
)


func handleHTTPConnection(downstream net.Conn) {
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
        readLines.PushBack(line)
        if strings.HasPrefix(line, "Host: ") {
            hostname = strings.TrimPrefix(line, "Host: ")
            break
        }
    }
    if hostname == "" {
        println("No host!")
        return
    }

    upstream, error := net.Dial("tcp", hostname + ":80")
    if error != nil {
        println("Couldn't connect to upstream", error)
        return
    }

    for element := readLines.Front(); element != nil; element = element.Next() {
        line := element.Value.(string)
        upstream.Write([]byte(line))
        upstream.Write([]byte("\n"))
    }

    go io.Copy(upstream, reader)
    go io.Copy(downstream, upstream)
}


func handleHTTPSConnection(downstream net.Conn) {
    firstByte := make([]byte, 1)
    _, error := downstream.Read(firstByte)
    if error != nil {
        println("Couldn't read first byte :-(")
        return
    }
    if firstByte[0] != 0x16 {
        println("Not TLS :-(")
    }

    versionBytes := make([]byte, 2)
    _, error = downstream.Read(versionBytes)
    if error != nil {
        println("Couldn't read version bytes :-(")
        return
    }
    if versionBytes[0] < 3 || (versionBytes[0] == 3 && versionBytes[1] < 1) {
        println("SSL < 3.1 so it's still not TLS")
        return
    }

    lengthBytes := make([]byte, 2)
    _, error = downstream.Read(lengthBytes)
    if error != nil {
        println("Couldn't read length bytes :-(")
        return
    }
    length := (lengthBytes[0] << 8) + (lengthBytes[1])
    println("Length is", length)
    
    rest := make([]byte, length)
    _, error = downstream.Read(rest)
    if error != nil {
        println("Couldn't read rest of bytes")
        return
    }
}


func doProxy(done chan int, port int, handle func(net.Conn)) {
    listener, error := net.Listen("tcp", "0.0.0.0:" + strconv.Itoa(port))
    if error != nil {
        println("Couldn't start listening", error)
    }
    println("Started proxy on", port, "-- listening...")
    for {
        connection, error := listener.Accept()
        if error != nil {
            println("Accept error", error)
            return
        }

        go handle(connection)
    }
    done <- 1
}


func main() {
    httpDone := make(chan int)
    go doProxy(httpDone, 80, handleHTTPConnection)

    httpsDone := make(chan int)
    go doProxy(httpsDone, 443, handleHTTPSConnection)

    <- httpDone
    <- httpsDone
}
