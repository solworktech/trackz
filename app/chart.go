package main

import (
	//"math/rand"
	"net/http"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"log"
	//"fmt"
	//"github.com/go-echarts/go-echarts/v2/types"
)

var dbPath = "/home/jesse/tmp/trackz-local/trackz.db"
func generatePieItems() []opts.PieData {
	items := make([]opts.PieData, 0)
    	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("select distinct process_name, window_name,  sum (focus_end_time - focus_start_time) as time from trackz group by process_name, window_name order by sum (focus_end_time - focus_start_time) desc limit 5")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var process_name string
		var window_name string
		var time int
		err = rows.Scan(&process_name, &window_name, &time)
		if err != nil {
			log.Fatal(err)
		}
		// fmt.Println(process_name)
		items = append(items,opts.PieData{Name: window_name, Value: time});
	}
	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
	/*
	items = append(items,opts.PieData{Name: "jesse@jessex: ~/tmp/trackz", Value: 1115});
	items = append(items,opts.PieData{Name: "ietf-sirius-rfcpa-exploration (Channel) - Sirius - Slack", Value: 72});
	items = append(items,opts.PieData{Name: "Watch Parks and Recreation Season 5 | Prime Video — Mozilla Firefox", Value: 280});
	items = append(items,opts.PieData{Name: "DOSBox 0.74-3, Cpu speed:     3000 cycles, Frameskip  0, Program:   BUBBOB", Value: 234});
	items = append(items,opts.PieData{Name: "PLATE Dashboard - Google Chrome", Value: 52});
	*/
	return items
}

func createPieChart(w http.ResponseWriter) {
    // create a new pie instance
    pie := charts.NewPie()
    pie.SetGlobalOptions(
        charts.WithTitleOpts(
            opts.Title{
                Title:    "Pie chart in Go",
                Subtitle: "This is fun to use!",
            },
        ),
    )
    pie.SetSeriesOptions()
    pie.AddSeries("Monthly revenue",
        generatePieItems()).
        SetSeriesOptions(
            charts.WithPieChartOpts(
                opts.PieChart{
                    Radius: 200,
                },
            ),
            charts.WithLabelOpts(
                opts.Label{
                    Show:      opts.Bool(true),
                    Formatter: "{b}: {c} ({d}%)",
                },
            ),
        )
    //f, _ := os.Create("pie.html")
    _ = pie.Render(w)
}

func httpserver(w http.ResponseWriter, _ *http.Request) {
	pie := charts.NewPie()
	//pie.AddJSFuncStrs(opts.FuncOpts(Formatter))
	pie.AddSeries("pie", generatePieItems()).
		SetSeriesOptions(charts.WithLabelOpts(
			opts.Label{
				Show:      opts.Bool(true),
				//Formatter: "{a}: {b}: {c}",
				//Formatter: opts.FuncOpts(Formatter),
			}),
		)
	pie.Render(w)
	//createPieChart(w)
}

func main() {
	http.HandleFunc("/", httpserver)
	http.ListenAndServe(":8081", nil)
}

