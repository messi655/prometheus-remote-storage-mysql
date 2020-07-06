package main

// Based on the Prometheus remote storage example:
// documentation/examples/remote_storage/remote_storage_adapter/main.go

import (
	"database/sql"
	"encoding/json"
	"flag"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"

	"strings"
	"time"

	"github.com/timescale/prometheus-postgresql-adapter/pkg/log"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"

	"fmt"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"

	_ "github.com/go-sql-driver/mysql"
)

type config struct {
	remoteTimeout     time.Duration
	listenAddr        string
	telemetryPath     string
	logLevel          string
	prometheusTimeout time.Duration
	dbDriver          string
	dbUser            string
	dbPassword        string
	dbName            string
	dbHost            string
	dbPort            string
	// telcoName         string
}

var dbConnection *sql.DB

func main() {
	cfg := parseFlags()

	dbConnection = dbConn(cfg.dbDriver, cfg.dbHost, cfg.dbPort, cfg.dbName, cfg.dbUser, cfg.dbPassword)
	defer dbConnection.Close()
	createTable()

	log.Init(cfg.logLevel)
	log.Info("config", fmt.Sprintf("%+v", cfg))

	http.Handle(cfg.telemetryPath, promhttp.Handler())

	http.Handle("/write", timeHandler("write", write()))

	log.Info("msg", "Starting up...")
	log.Info("msg", "Listening", "addr", cfg.listenAddr)

	err := http.ListenAndServe(cfg.listenAddr, nil)

	if err != nil {
		log.Error("msg", "Listen failure", "err", err)
		os.Exit(1)
	}
}

func parseFlags() *config {

	cfg := &config{}

	flag.DurationVar(&cfg.remoteTimeout, "adapter-send-timeout", 30*time.Second, "The timeout to use when sending samples to the remote storage.")
	flag.StringVar(&cfg.listenAddr, "web-listen-address", ":9201", "Address to listen on for web endpoints.")
	flag.StringVar(&cfg.telemetryPath, "web-telemetry-path", "/metrics", "Address to listen on for web endpoints.")
	flag.StringVar(&cfg.logLevel, "log-level", "debug", "The log level to use [ \"error\", \"warn\", \"info\", \"debug\" ].")
	flag.DurationVar(&cfg.prometheusTimeout, "leader-election-pg-advisory-lock-prometheus-timeout", -1, "Adapter will resign if there are no requests from Prometheus within a given timeout (0 means no timeout). "+
		"Note: make sure that only one Prometheus instance talks to the adapter. Timeout value should be co-related with Prometheus scrape interval but add enough `slack` to prevent random flips.")
	flag.StringVar(&cfg.dbDriver, "db-driver", "mysql", "driver database server.")
	flag.StringVar(&cfg.dbHost, "db-address", "localhost", "Address of mysql database server.")
	flag.StringVar(&cfg.dbPort, "db-port", "3306", "mysql database server port.")
	flag.StringVar(&cfg.dbName, "db-name", "monitoring", "Database name of mysql database.")

	flag.StringVar(&cfg.dbUser, "db-user", "monitoring", "username of database.")
	flag.StringVar(&cfg.dbPassword, "db-password", "monitoring", "password of database.")

	flag.Parse()

	return cfg
}

//MetricResult is struct for test
type MetricResult struct {
	ColumnName string `json:"column_name"`
	DateTime   string `json:"date_time"`
}

func dbConn(dbDriver string, dbHost string, dbPort string, dbName string, dbUser string, dbPassword string) (db *sql.DB) {

	db, err := sql.Open(dbDriver, dbUser+":"+dbPassword+"@tcp("+dbHost+":"+dbPort+")/"+dbName)
	if err != nil {
		panic(err.Error())
	}
	return db
}

func createTable() {
	query := "CREATE TABLE IF NOT EXISTS monitoring( id int NOT NULL auto_increment,date_time date DEFAULT NULL,metrics_name varchar(255) DEFAULT NULL,value BIGINT DEFAULT NULL,primary key(id)) ENGINE=InnoDB DEFAULT CHARSET=latin1"

	stmt, es := dbConnection.Prepare(query)
	if es != nil {
		panic(es.Error())
	}

	_, er := stmt.Exec()
	if er != nil {
		panic(er.Error())
	}
}

//insert to db
func insert(datetime string, columnname string, value string) {

	query := "insert into monitoring(date_time,metrics_name,value) select * from (select" + "'" + datetime + "'" + "," + "'" + columnname + "'" + "," + "'" + value + "'" + ") as tmp where not exists (select date_time,metrics_name,value from monitoring where date_time = ? AND metrics_name = ? AND value = ? )"

	stmt, es := dbConnection.Prepare(query)
	if es != nil {
		panic(es.Error())
	}

	_, er := stmt.Exec(datetime, columnname, value)
	if er != nil {
		panic(er.Error())
	}

}

func write() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		compressed, err := ioutil.ReadAll(r.Body)

		if err != nil {
			log.Error("msg", "Read error", "err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		reqBuf, err := snappy.Decode(nil, compressed)

		if err != nil {
			log.Error("msg", "Decode error", "err", err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var req prompb.WriteRequest
		if err := proto.Unmarshal(reqBuf, &req); err != nil {
			log.Error("msg", "Unmarshal error", "err", err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		samples := protoToSamples(&req)

		for t := range samples {
			sampleResult := samples[t]
			if strings.Contains(sampleResult.Metric.String(), "simba_metric") {
				s, _ := json.Marshal(sampleResult.Metric)
				var metricResult MetricResult
				json.Unmarshal(s, &metricResult)
				if len(metricResult.DateTime) > 0 && len(metricResult.ColumnName) > 0 && sampleResult.Value > 0 {
					insert(metricResult.DateTime, metricResult.ColumnName, fmt.Sprintf("%f", sampleResult.Value))
				} else {
					log.Info("Metric result is emty")
				}

			}

		}

	})
}

func protoToSamples(req *prompb.WriteRequest) model.Samples {
	var samples model.Samples
	for _, ts := range req.Timeseries {

		metric := make(model.Metric, len(ts.Labels))
		for _, l := range ts.Labels {
			metric[model.LabelName(l.Name)] = model.LabelValue(l.Value)
		}

		for _, s := range ts.Samples {

			samples = append(samples, &model.Sample{
				Metric:    metric,
				Value:     model.SampleValue(s.Value),
				Timestamp: model.Time(s.Timestamp),
			})

		}
	}
	return samples
}

// timeHandler uses Prometheus histogram to track request time
func timeHandler(path string, handler http.Handler) http.Handler {
	f := func(w http.ResponseWriter, r *http.Request) {

		handler.ServeHTTP(w, r)

	}
	return http.HandlerFunc(f)
}
