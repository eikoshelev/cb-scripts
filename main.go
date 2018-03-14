package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/couchbase/gocb"
	"github.com/miekg/dns"
)

var bucket *gocb.Bucket

type Inventory struct {
	Ip     string            `json:"ip,omitempty"`
	Tag    []string          `json:"tag,omitempty"`
	Apps   []string          `json:"apps,omitempty"`
	Active bool              `json:"active,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

func main() {

	Config = configure()

	flag.Parse()

	Logger, err := initLogger()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	Logger.Infof("Started!\n")

	conn, err := gocb.Connect(Config.Storage.Hosts[0])
	if err != nil {
		Logger.Errorf("Failed connect to host: %s", err)
	}
	err = conn.Authenticate(gocb.PasswordAuthenticator{Config.Storage.Login, Config.Storage.Password})
	if err != nil {
		Logger.Errorf("Failed authenticate: %s", err)
	}

	bucket, err = conn.OpenBucket(Config.Storage.Bucket, "")
	if err != nil {
		Logger.Errorf("Failed open bucket: %s", err)
	}

	http.HandleFunc("/manager/", manager)
	http.HandleFunc("/search/", search)

	err = http.ListenAndServe(":"+Config.Server.Http.Port, nil)
	if err != nil {
		Logger.Fatalf("ListenAndServe: %s", err)
	}

	server := dns.Server{Addr: ":" + Config.Server.Dns.Port, Net: Config.Server.Dns.Network}

	/*
		config, err := dns.ClientConfigFromFile("/etc/resolv.conf")
		if err != nil {
			Logger.Fatalf("Can`t read file '/etc/resolv.conf': %s", err.Error())
		}
		c := new(dns.Client)

		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(os.Args[1]), dns.TypeMX)
		m.RecursionDesired = true

		r, _, err := c.Exchange(m, net.JoinHostPort(config.Servers[0], config.Port))

		if r == nil {
			Logger.Fatalf("Error: %s\n", err.Error())
		}

		if r.Rcode != dns.RcodeSuccess {
			Logger.Fatalf("Invalid answer name %s after MX query for %s\n", os.Args[1], os.Args[1])
		}

		for _, a := range r.Answer {
			fmt.Printf("%v\n", a)
		}
	*/

	go func() {
		if err := server.ListenAndServe(); err != nil {
			Logger.Fatalf("Failed to set udp listener %s", err)
		}
	}()

	dns.HandleFunc(".", handleRequest)

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	Logger.Fatalf("Signal (%v) received, stopping\n", s)
}

func handleRequest(w dns.ResponseWriter, r *dns.Msg) {

	m := new(dns.Msg)
	fmt.Println("handleRequest:inbound message:")
	fmt.Printf("%+v", r)
	for _, q := range r.Question {
		name := q.Name
		fmt.Println(name)

		var host Inventory

		_, err := bucket.Get(name[:len(name)-1], &host)

		if err != nil {
			Logger.Debugf("Failed get: %s", name[:len(name)-1])
			fmt.Println(name, err)
			m.SetReply(r)
			fmt.Println(m.Answer)
			w.WriteMsg(m)
			return
		}

		answer := new(dns.A)
		answer.Hdr = dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: Config.Server.Dns.Ttl}
		answer.A = net.ParseIP(host.Ip)
		m.Answer = append(m.Answer, answer)
	}
	m.SetReply(r)
	fmt.Printf("%+v\n", m)
	w.WriteMsg(m)
}
