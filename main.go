package main

import (
	"mygo-video/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		app.ReportFatal(err)
	}
}
