package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	if err := run(); err != nil {
		log.Fatalln(err)
	}
}

func run() (err error) {
	// SIGINT（CTRL+C）を適切に処理するようにします。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// OpenTelemetryのセットアップ。
	otelShutdown, err := setupOTelSDK(ctx)
	if err != nil {
		return
	}
	// リークが発生しないよう、適切にシャットダウン処理を行います。
	defer func() {
		err = errors.Join(err, otelShutdown(context.Background()))
	}()

	// HTTPサーバーを起動。
	srv := &http.Server{
		Addr:         ":8080",
		BaseContext:  func(_ net.Listener) context.Context { return ctx },
		ReadTimeout:  time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      newHTTPHandler(),
	}
	srvErr := make(chan error, 1)
	go func() {
		srvErr <- srv.ListenAndServe()
	}()

	// 割り込みを待機する。
	select {
	case err = <-srvErr:
		// Error when starting HTTP server.
		// HTTPサーバーの起動中のエラー。
		return
	case <-ctx.Done():
		// 最初の CTRL+C を待機します。
		// 可能な限り早くシグナル通知の受信を停止します。
		stop()
	}

	// Shutdownが呼び出されると、ListenAndServeは即座にErrServerClosedを返します。
	err = srv.Shutdown(context.Background())
	return
}

func newHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// handleFuncはmux.HandleFuncの代替であり、
	// ハンドラーのHTTP計装において、パターンをhttp.routeとして付加します。
	handleFunc := func(pattern string, handlerFunc func(http.ResponseWriter, *http.Request)) {
		// Configure the "http.route" for the HTTP instrumentation.
		handler := otelhttp.WithRouteTag(pattern, http.HandlerFunc(handlerFunc))
		mux.Handle(pattern, handler)
	}

	// ハンドラーの登録。
	handleFunc("/rolldice/", rolldice)
	handleFunc("/rolldice/{player}", rolldice)

	// サーバー全体に対してHTTP計装を追加します。
	handler := otelhttp.NewHandler(mux, "/")
	return handler
}
