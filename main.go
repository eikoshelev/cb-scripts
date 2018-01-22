package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/couchbase/gocb"
	"github.com/go-yaml/yaml"
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

type Conf struct {
	Server struct {
		Http struct {
			Http_port string `yaml:"http_port"`
		} `yaml:"http"`
		Dns struct {
			Dns_port string `yaml:"dns_port"`
			Network  string `yaml:"network"`
			Ttl      uint32 `yaml:"ttl"`
		} `yaml:"dns"`
	} `yaml:"server"`

	Storage struct {
		Login    string   `yaml:"login"`
		Password string   `yaml:"password"`
		Bucket   string   `yaml:"bucket"`
		Hosts    []string `yaml:"hosts"`
	} `yaml:"storage"`
}

var configFlag = flag.String("config", "./default.yaml", "set config file in the yaml format")

var config Conf

func configure() Conf {

	configFile, err := ioutil.ReadFile(*configFlag)
	if err != nil {
		fmt.Println("Failed read configuration file: ", err)
	}

	var c Conf
	err = yaml.Unmarshal(configFile, &c)

	if err != nil {
		fmt.Println("Failed unmarshal ", *configFlag, err)
	}

	return c
}

func main() {

	flag.Parse()
	config = configure()
	fmt.Printf("Configuration:%+v\n", config)

	conn, err := gocb.Connect(config.Storage.Hosts[0])
	if err != nil {
		fmt.Println("Failed connect to host", err)
	}
	_ = conn.Authenticate(gocb.PasswordAuthenticator{config.Storage.Login, config.Storage.Password})
	bucket, err = conn.OpenBucket(config.Storage.Bucket, "")
	if err != nil {
		fmt.Println("Failed open bucket: ", *bucket, err)
	}

	http.HandleFunc("/manager/", manager)

	errr := http.ListenAndServe(":"+config.Server.Http.Http_port, nil)
	if errr != nil {
		log.Fatal("ListenAndServe: ", err)
	}

	server := &dns.Server{Addr: ":" + config.Server.Dns.Dns_port, Net: config.Server.Dns.Network}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Failed to set udp listener %s\n", err.Error())
		}
	}()

	dns.HandleFunc(".", handleRequest)

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Fatalf("Signal (%v) received, stopping\n", s)
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
			fmt.Println(name, err)
			m.SetReply(r)
			fmt.Println(m.Answer)
			w.WriteMsg(m)
			return
		}

		answer := new(dns.A)
		answer.Hdr = dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: config.Server.Dns.Ttl}
		answer.A = net.ParseIP(host.Ip)
		m.Answer = append(m.Answer, answer)
	}
	m.SetReply(r)
	fmt.Printf("%+v\n", m)
	w.WriteMsg(m)
}

func manager(w http.ResponseWriter, r *http.Request) {

	type Cas gocb.Cas

	met := r.Method

	switch met {
	case "GET":

		var document Inventory

		doc := r.URL.Path[len("/manager/"):]
		_, error := bucket.Get(doc, &document)
		if error != nil {
			fmt.Println(error.Error())
			return
		}
		jsonDocument, error := json.Marshal(&document)
		if error != nil {
			fmt.Println(error.Error())
		}
		fmt.Fprintf(w, "%v\n", string(jsonDocument))

	case "POST":

		var result Inventory

		doc := r.URL.Path[len("/manager/"):]
		body, error := ioutil.ReadAll(r.Body)
		if error != nil {
			fmt.Println(error.Error())
		}
		error = json.Unmarshal(body, &result)
		if error != nil {
			fmt.Println(w, "can't unmarshal: ", doc, error)
		} else {
			bucket.Upsert(doc, result, 0)
		}

	case "DELETE":

		doc := r.URL.Path[len("/manager/"):]
		bucket.Remove(doc, 0)

	//TODO: поле params при апдейте не заменяет, а добавляет значения, т.к. ключи не описаны как omitempty. Разобраться (!)
	case "UPDATE":

		doc := r.URL.Path[len("/manager/"):]

		var document Inventory

		cas, error := bucket.GetAndLock(doc, 000, &document) //TODO: set time lock
		if error != nil {
			fmt.Println(error.Error()) //TODO: обработка ошибки
		}
		body, error := ioutil.ReadAll(r.Body)
		if error != nil {
			fmt.Println(error.Error()) //TODO: обработка ошибки
		}
		error = json.Unmarshal(body, &document)
		if error != nil {
			fmt.Println(w, "can't unmarshal: ", doc, error) //TODO: обработка ошибки
		}

		cas, error = bucket.Replace(doc, &document, cas, 0)
		if error != nil {
			fmt.Println(error.Error())
		}
		bucket.Unlock(doc, cas)

	default:

		fmt.Println("Error: ", "\"", met, "\"", " - unknown method. Using GET, POST, DELETE, UPDATE method.")
	}
}
