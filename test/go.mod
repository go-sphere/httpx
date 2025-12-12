module github.com/go-sphere/httpx/test

go 1.25

require (
	github.com/go-sphere/httpx v0.0.0
	github.com/go-sphere/httpx/fiberx v0.0.0
	github.com/go-sphere/httpx/ginx v0.0.0
	github.com/go-sphere/httpx/hertzx v0.0.0
)

replace github.com/go-sphere/httpx => ../
replace github.com/go-sphere/httpx/fiberx => ../fiberx
replace github.com/go-sphere/httpx/ginx => ../ginx
replace github.com/go-sphere/httpx/hertzx => ../hertzx
