# Prometheus remote storage adapter for MySQL

## How to run in Testing environment

### 1. Start docker

- Go to docker folder

```
docker-compose up -d
```

### 2. Run Adapter

```
go run main.go -db-user root -db-password 1234 -db-name monitoring -db-port 7306
```

### 3. Send data to statsd_exporter

```
echo -n "simba_metric.indosat_num_msisdn_rows_distinct.2020-08-28:3811110112|g" | nc -w 1 -u localhost 9125
```

### 4. How to check

- Go to and find simba_metric
```
- StatsD Exporter: http://localhost:9102/metrics
- Prometheus: http://localhost:9090/graph
```

- Or go to MySQL database to check