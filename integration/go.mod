module github.com/go-sphere/httpx/integration

go 1.25.5

replace (
	github.com/go-sphere/httpx => ../
	github.com/go-sphere/httpx/echox => ../echox
	github.com/go-sphere/httpx/fiberx => ../fiberx
	github.com/go-sphere/httpx/ginx => ../ginx
	github.com/go-sphere/httpx/hertzx => ../hertzx
	github.com/go-sphere/httpx/testing => ../testing
)

require (
	github.com/go-sphere/httpx v0.0.2-beta.24
	github.com/go-sphere/httpx/testing v0.0.0-00010101000000-000000000000
)
