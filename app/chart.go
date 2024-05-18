package main

import (
	"net/http"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	//"github.com/go-echarts/go-echarts/v2/types"
)


func generatePieItems() []opts.PieData {
	items := make([]opts.PieData, 0)
	items = append(items,opts.PieData{Name: "jesse@jessex: ~/tmp/trackz", Value: 1115});
	items = append(items,opts.PieData{Name: "DOSBox 0.74-3, Cpu speed:     3000 cycles, Frameskip  0, Program:   BUBBOB", Value: 234});
	return items
}

func createPieChart(w http.ResponseWriter) {
    // create a new pie instance
    pie := charts.NewPie()
    pie.SetGlobalOptions(
        charts.WithTitleOpts(
            opts.Title{
                Title:    "App in Focus",
                // Subtitle: "This is fun to use!",
            },
        ),
    )
    pie.SetSeriesOptions()
    pie.AddSeries("App in Focus",
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

