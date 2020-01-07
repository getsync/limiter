package fasthttp_test

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/middleware/fasthttp"
	"github.com/ulule/limiter/v3/drivers/store/memory"
	libFastHttp "github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFasthttpMiddleware(t *testing.T) {
	is := require.New(t)

	store := memory.NewStore()
	is.NotZero(store)

	rate, err := limiter.NewRateFromFormatted("10-M")
	is.NoError(err)
	is.NotZero(rate)

	middleware := fasthttp.NewMiddleware(limiter.New(store, rate))

	requestHandler := func(ctx *libFastHttp.RequestCtx) {
		switch string(ctx.Path()) {
		case "/":
			ctx.SetStatusCode(libFastHttp.StatusOK)
			ctx.SetBodyString("hello")
		break
		}
	}

	success := int64(10)
	clients := int64(100)

	//
	// Sequential
	//

	for i := int64(1); i <= clients; i++ {
		resp := libFastHttp.AcquireResponse()
		req := libFastHttp.AcquireRequest()
		req.Header.SetHost("localhost:8081")
		req.Header.SetRequestURI("/")
		err := serve(middleware.Handle(requestHandler), req, resp)
		is.Nil(err)

		if i <= success {
			is.Equal(resp.StatusCode(), libFastHttp.StatusOK)
		} else {
			is.Equal(resp.StatusCode(), libFastHttp.StatusTooManyRequests)
		}
	}

	//
	// Concurrent
	//

	store = memory.NewStore()
	is.NotZero(store)

	middleware = fasthttp.NewMiddleware(limiter.New(store, rate))

	requestHandler = func(ctx *libFastHttp.RequestCtx) {
		switch string(ctx.Path()) {
		case "/":
			ctx.SetStatusCode(libFastHttp.StatusOK)
			ctx.SetBodyString("hello")
			break
		}
	}

	wg := &sync.WaitGroup{}
	counter := int64(0)

	for i := int64(1); i <= clients; i++ {
		wg.Add(1)

		go func() {
			resp := libFastHttp.AcquireResponse()
			req := libFastHttp.AcquireRequest()
			req.Header.SetHost("localhost:8081")
			req.Header.SetRequestURI("/")
			err := serve(middleware.Handle(requestHandler), req, resp)
			is.Nil(err)

			if resp.StatusCode() == libFastHttp.StatusOK {
				atomic.AddInt64(&counter, 1)
			}

			wg.Done()
		}()
	}

	wg.Wait()
	is.Equal(success, atomic.LoadInt64(&counter))

	//
	// Custom KeyGetter
	//

	store = memory.NewStore()
	is.NotZero(store)

	j := 0
	KeyGetter := func(ctx *libFastHttp.RequestCtx) string {
		j++
		return strconv.Itoa(j)
	}
	middleware = fasthttp.NewMiddleware(limiter.New(store, rate), fasthttp.WithKeyGetter(KeyGetter))

	is.NotZero(middleware)

	requestHandler = func(ctx *libFastHttp.RequestCtx) {
		switch string(ctx.Path()) {
		case "/":
			ctx.SetStatusCode(libFastHttp.StatusOK)
			ctx.SetBodyString("hello")
			break
		}
	}

	for i := int64(1); i <= clients; i++ {
		resp := libFastHttp.AcquireResponse()
		req := libFastHttp.AcquireRequest()
		req.Header.SetHost("localhost:8081")
		req.Header.SetRequestURI("/")
		err := serve(middleware.Handle(requestHandler), req, resp)
		is.Nil(err)
		is.Equal(libFastHttp.StatusOK, resp.StatusCode(), strconv.Itoa(int(i)))
	}
}

func serve(handler libFastHttp.RequestHandler, req *libFastHttp.Request, res *libFastHttp.Response) error {
	ln := fasthttputil.NewInmemoryListener()
	defer ln.Close()

	go func() {
		err := libFastHttp.Serve(ln, handler)
		if err != nil {
			panic(err)
		}
	}()

	client := libFastHttp.Client{
		Dial: func(addr string) (net.Conn, error) {
			return ln.Dial()
		},
	}

	return client.Do(req, res)
}
