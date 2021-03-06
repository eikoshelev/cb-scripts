package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"

	"github.com/couchbase/gocb"
	"github.com/couchbase/gocb/cbft"
)

func search(w http.ResponseWriter, r *http.Request) {

	var answer []inventory

	Logger, _ := initLogger()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		Logger.Errorf("SEARCH: Incorrect body request: %s", err)
		fmt.Fprintf(w, "SEARCH: Incorrect body request: %s \n", err)
	}

	search := make(map[string]interface{})

	err = json.Unmarshal(body, &search)
	if err != nil {
		Logger.Errorf("SEARCH: Can`t unmarshal: %s", err)
		fmt.Fprintf(w, "SEARCH: Can`t unmarshal: %s \n", err)
	}

	// слайс для хранения запроса
	res := []cbft.FtsQuery{}

	// получаем имя дока
	doc := r.URL.Path[len("/search/"):]

	// если имя дока было указано - добавляем в запрос
	if doc != "" {
		res = append(res, cbft.NewDocIdQuery(doc))
	}

	for key, val := range search {

		switch valt := val.(type) {

		case string: // IP
			res = append(res, cbft.NewPhraseQuery(valt).Field(key))

		case []interface{}: // Tag and/or Apps
			for _, item := range valt {
				if s, ok := item.(string); ok {
					res = append(res, cbft.NewPhraseQuery(s).Field(key))
				}
			}

		case bool: // Active (!)
			res = append(res, cbft.NewBooleanFieldQuery(valt).Field(key))

		case map[string]interface{}: // Params
			for _, item := range valt {
				if s, ok := item.(string); ok {
					res = append(res, cbft.NewPhraseQuery(s).Field(key))
				}
			}
		}
	}

	// распаковываем слайс
	query := cbft.NewConjunctionQuery(res...)

	req := gocb.NewSearchQuery("search-index", query)

	// отправляем запрос
	rows, err := bucket.ExecuteSearchQuery(req)
	if err != nil {
		totalRequestHttp.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		Logger.Errorf("SEARCH: Failed to send request: %s", err)
		fmt.Fprintf(w, "SEARCH: Failed to send request: %s \n", err)
	} else {
		totalRequestHttp.WithLabelValues(strconv.Itoa(http.StatusOK)).Inc()
	}
	// получаем все подходящие документы по их id
	for _, hit := range rows.Hits() {

		var ans inventory

		_, err := bucket.Get(hit.Id, &ans)
		if err != nil {
			totalRequestHttp.WithLabelValues(strconv.Itoa(http.StatusNotFound)).Inc()
			Logger.Errorf("SEARCH: Failed to get note: %s", err)
			fmt.Fprintf(w, "SEARCH: Failed to get note: %s \n", err)
		} else {
			totalRequestHttp.WithLabelValues(strconv.Itoa(http.StatusOK)).Inc()
		}
		answer = append(answer, ans)

	}

	jsonDocument, err := json.Marshal(&answer)
	if err != nil {
		Logger.Errorf("SEARCH: Can`t marshal: %s", err)
		fmt.Fprintf(w, "SEARCH: Can`t marshal: %s \n", err)
	}
	fmt.Fprintf(w, "%s \n", string(jsonDocument))
}
