package metalbond

import (
	"encoding/json"
	"fmt"
	"net/http"
	"text/template"
	"time"

	yaml "gopkg.in/yaml.v2"
)

type jsonRoutes struct {
	Date             string                         `json:"date"`
	VNet             map[uint32]map[string][]string `json:"vnet"`
	MetalBondVersion string                         `json:"metalbondVersion"`
}

type jsonServer struct {
	m *MetalBond
}

func serveJsonRouteTable(m *MetalBond, listen string) error {
	js := jsonServer{
		m: m,
	}

	http.HandleFunc("/", js.mainHandler)
	http.HandleFunc("/routes.json", js.jsonHandler)
	http.HandleFunc("/routes.yaml", js.yamlHandler)

	http.ListenAndServe(listen, nil)

	return nil
}

func (j *jsonServer) getJsonRoutes() (jsonRoutes, error) {
	js := jsonRoutes{
		MetalBondVersion: METALBOND_VERSION,
		Date:             time.Now().Format("2006-01-02 15:04:05"),
		VNet:             make(map[uint32]map[string][]string),
	}

	for vni, rt := range j.m.routeTables {
		js.VNet[uint32(vni)] = make(map[string][]string)
		for dst, hops := range rt.Routes {
			for hop := range hops {
				js.VNet[uint32(vni)][dst.String()] = append(js.VNet[uint32(vni)][dst.String()], hop.String())
			}
		}
	}

	return js, nil
}

func (j *jsonServer) mainHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.New("index.html").ParseFiles("html/index.html")
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	js, err := j.getJsonRoutes()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	err = tmpl.Execute(w, js)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
}

func (j *jsonServer) jsonHandler(w http.ResponseWriter, r *http.Request) {
	js, err := j.getJsonRoutes()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	out, err := json.MarshalIndent(js, "", "  ")
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	w.Write(out)
}

func (j *jsonServer) yamlHandler(w http.ResponseWriter, r *http.Request) {
	js, err := j.getJsonRoutes()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	out, err := yaml.Marshal(js)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error: %v", err)
		return
	}

	w.Header().Add("Content-Type", "text/yaml")
	w.Write(out)
}
