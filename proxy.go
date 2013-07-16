package main

import (
    "bufio"
    "container/list"
    "errors"
    "fmt"
    "github.com/fzzy/radix/redis"
    "io"
    "log"
    "net"
    "os"
    "math/rand"
    "strconv"
    "strings"
)



func getBackend(hostname string, fallback string, redisClient *redis.Client) (string, error) {
    fmt.Println("Looking up", hostname)

    backends, error := redisClient.Cmd("smembers", "hostnames:" + hostname + ":backends").List()
    if error != nil {
        fmt.Println("Error in redis lookup", error)
        return "", error
    }

    if len(backends) == 0 {
        backends, error = redisClient.Cmd("smembers", "hostnames:" + fallback + ":backends").List()
        if len(backends) == 0 {
            fmt.Println("No fallback of type", fallback)
            return "", errors.New("Could not find fallback of type " + fallback)
        }
    }
    if error != nil {
        fmt.Println("Error in redis lookup", error)
        return "", error
    }

    fmt.Println("Found backends:", backends)
    backend := backends[int(rand.Float32() * float32(len(backends)))]
    return backend, nil
}


func handleHTTPConnection(downstream net.Conn, redisClient *redis.Client) {
    reader := bufio.NewReader(downstream)
    hostname := ""
    readLines := list.New()
    for hostname == "" {
        bytes, _, error := reader.ReadLine()
        if error != nil {
            fmt.Println("Error reading")
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
        fmt.Println("No host!")
        return
    }
    backendAddress, error := getBackend(hostname, "httpFallback", redisClient)
    if error != nil {
        fmt.Println("Couldn't get backend for ", hostname, "-- got error", error)
        return
    }

    upstream, error := net.Dial("tcp", backendAddress + ":80")
    if error != nil {
        fmt.Println("Couldn't connect to upstream", error)
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


func handleHTTPSConnection(downstream net.Conn, redisClient *redis.Client) {
    firstByte := make([]byte, 1)
    _, error := downstream.Read(firstByte)
    if error != nil {
        fmt.Println("Couldn't read first byte :-(")
        return
    }
    if firstByte[0] != 0x16 {
        fmt.Println("Not TLS :-(")
    }

    versionBytes := make([]byte, 2)
    _, error = downstream.Read(versionBytes)
    if error != nil {
        fmt.Println("Couldn't read version bytes :-(")
        return
    }
    if versionBytes[0] < 3 || (versionBytes[0] == 3 && versionBytes[1] < 1) {
        fmt.Println("SSL < 3.1 so it's still not TLS")
        return
    }

    restLengthBytes := make([]byte, 2)
    _, error = downstream.Read(restLengthBytes)
    if error != nil {
        fmt.Println("Couldn't read restLength bytes :-(")
        return
    }
    restLength := (int(restLengthBytes[0]) << 8) + int(restLengthBytes[1])
    
    rest := make([]byte, restLength)
    _, error = downstream.Read(rest)
    if error != nil {
        fmt.Println("Couldn't read rest of bytes")
        return
    }

    current := 0

    handshakeType := rest[0]
    current += 1
    if handshakeType != 0x1 {
        fmt.Println("Not a ClientHello")
        return
    }

    // Skip over another length
    current += 3
    // Skip over protocolversion
    current += 2
    // Skip over random number
    current += 4 + 28
    // Skip over session ID
    sessionIDLength := int(rest[current])
    current += 1
    current += sessionIDLength
    
    cipherSuiteLength := (int(rest[current]) << 8) + int(rest[current + 1])
    current += 2
    current += cipherSuiteLength

    compressionMethodLength := int(rest[current])
    current += 1
    current += compressionMethodLength

    if current > restLength {
        fmt.Println("no extensions")
        return
    }

    // Skip over extensionsLength
    // extensionsLength := (int(rest[current]) << 8) + int(rest[current + 1])
    current += 2

    hostname := ""
    for current < restLength && hostname == "" {
        extensionType := (int(rest[current]) << 8) + int(rest[current + 1])
        current += 2
   
        extensionDataLength := (int(rest[current]) << 8) + int(rest[current + 1])
        current += 2
        
        if extensionType == 0 {

            // Skip over number of names as we're assuming there's just one
            current += 2

            nameType := rest[current]
            current += 1
            if nameType != 0 {
                fmt.Println("Not a hostname") 
                return
            }
            nameLen := (int(rest[current]) << 8) + int(rest[current+1])
            current += 2
            hostname = string(rest[current:current + nameLen])
        }

        current += extensionDataLength
    }
    if hostname == "" {
        fmt.Println("No hostname")
        return
    }
    
    backendAddress, error := getBackend(hostname, "httpsFallback", redisClient)
    if error != nil {
        fmt.Println("Couldn't get backend for ", hostname, "-- got error", error)
        return
    }

    upstream, error := net.Dial("tcp", backendAddress + ":443")
    if error != nil {
        log.Fatal(error)
        return
    }

    upstream.Write(firstByte)
    upstream.Write(versionBytes)
    upstream.Write(restLengthBytes)
    upstream.Write(rest)

    go io.Copy(upstream, downstream)
    go io.Copy(downstream, upstream)
}


func doProxy(done chan int, port int, handle func(net.Conn, *redis.Client), redisClient *redis.Client) {
    listener, error := net.Listen("tcp", "0.0.0.0:" + strconv.Itoa(port))
    if error != nil {
        fmt.Println("Couldn't start listening", error)
    }
    fmt.Println("Started proxy on", port, "-- listening...")
    for {
        connection, error := listener.Accept()
        if error != nil {
            fmt.Println("Accept error", error)
            return
        }

        go handle(connection, redisClient)
    }
    done <- 1
}


func main() {
    redisClient, error := redis.Dial("tcp", "127.0.0.1:6379")
    if error != nil {
        fmt.Println("Error connecting to redis", error)
        os.Exit(1)
    }

    httpDone := make(chan int)
    go doProxy(httpDone, 80, handleHTTPConnection, redisClient)

    httpsDone := make(chan int)
    go doProxy(httpsDone, 443, handleHTTPSConnection, redisClient)

    <- httpDone
    <- httpsDone
}
