package main

import (
    "bufio"
    "container/list"
    "io"
    "log"
    "net"
    "strconv"
    "strings"
)


func getBackend(hostname string) string {
    println("Getting backend for '", hostname, "'")
    ba := []byte(hostname)
    for i:=0; i < len(ba); i++ {
        println("Byte", i, "is", ba[i])
    }
    if hostname == "www.dilbert.com" {
        return "184.106.169.31"
    } else if hostname == "www.gilesthomas.com" {
        return "212.110.190.213"
    } else if hostname == "www.pythonanywhere.com" {
        return "50.19.109.98"
    }
    return "8.8.8.8"
}

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
    backendAddress := getBackend(hostname)

    println("Making upstream connection to", backendAddress + ":80")
    upstream, error := net.Dial("tcp", backendAddress + ":80")
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

    restLengthBytes := make([]byte, 2)
    _, error = downstream.Read(restLengthBytes)
    if error != nil {
        println("Couldn't read restLength bytes :-(")
        return
    }
    restLength := (int(restLengthBytes[0]) << 8) + int(restLengthBytes[1])
    println("restLength is", restLength)
    
    rest := make([]byte, restLength)
    _, error = downstream.Read(rest)
    if error != nil {
        println("Couldn't read rest of bytes")
        return
    }

    current := 0

    handshakeType := rest[0]
    current += 1
    if handshakeType != 0x1 {
        println("Not a ClientHello")
        return
    }

    // Skip over another length
    current += 3
    // Skip over protocolversion
    println("Protocolversion is", rest[current], rest[current+1])
    current += 2
    // Skip over random number
    current += 4 + 28
    // Skip over session ID
    sessionIDLength := int(rest[current])
    current += 1
    println("session ID length", sessionIDLength)
    current += sessionIDLength
    
    cipherSuiteLength := (int(rest[current]) << 8) + int(rest[current + 1])
    current += 2
    println("CipherSuite length is", cipherSuiteLength)
    current += cipherSuiteLength

    compressionMethodLength := int(rest[current])
    current += 1
    println("CompressionMethodLength is", compressionMethodLength)
    current += compressionMethodLength

    if current > restLength {
        println("no extensions")
        return
    }

    extensionsLength := (int(rest[current]) << 8) + int(rest[current + 1])
    current += 2
    println("ExtensionsLength", extensionsLength)

    hostname := ""
    for current < restLength && hostname == "" {
        extensionType := (int(rest[current]) << 8) + int(rest[current + 1])
        current += 2
        println("Extension type", extensionType)
   
        extensionDataLength := (int(rest[current]) << 8) + int(rest[current + 1])
        current += 2
        println("Extension data length", extensionDataLength)
        
        if extensionType == 0 {
            println("It's an SNI")

            // Skip over number of names as we're assuming there's just one
            current += 2

            nameType := rest[current]
            current += 1
            if nameType != 0 {
                println("Not a hostname") 
                return
            }
            nameLen := (int(rest[current]) << 8) + int(rest[current+1])
            current += 2
            println("Name length", nameLen)
            hostname = string(rest[current:current + nameLen])
            println("got a name:", hostname)
        } else {
            println("Not an SNI")
        }

        current += extensionDataLength
    }
    if hostname == "" {
        println("No hostname")
        return
    }
    
    backendAddress := getBackend(hostname)
    println("Connecting via TCP to", backendAddress + ":443")
    upstream, error := net.Dial("tcp", backendAddress + ":443")
    if error != nil {
        log.Fatal(error)
        return
    }
    println("Connected, replaying header")

    upstream.Write(firstByte)
    upstream.Write(versionBytes)
    upstream.Write(restLengthBytes)
    upstream.Write(rest)

    println("Header replayed, going live")
    go io.Copy(upstream, downstream)
    go io.Copy(downstream, upstream)
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
