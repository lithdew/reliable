module github.com/lithdew/reliable

go 1.14

replace github.com/lithdew/seq => ../seq

require (
	github.com/davecgh/go-spew v1.1.0
	github.com/lithdew/bytesutil v0.0.0-20200409052507-d98389230a59
	github.com/lithdew/seq v0.0.0-20200504075926-2385e7ddfbde
	github.com/stretchr/testify v1.5.1
	github.com/valyala/bytebufferpool v1.0.0
	go.uber.org/goleak v1.0.0
	golang.org/x/lint v0.0.0-20200302205851-738671d3881b // indirect
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	golang.org/x/tools v0.0.0-20200501005904-d351ea090f9b // indirect
)
