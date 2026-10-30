package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"

	"github.com/julienschmidt/httprouter"
)

func addHealthHandlers(router *httprouter.Router) {
	router.GET("/health", makeRequestHandlerWithContext(handleHealth))
}

func handleHealth(ctx context.Context, w http.ResponseWriter, _ httprouter.Params) error {
	// This is a blind yes/no response
	var err error
	if config.EnhancedCheck {
		if db == nil {
			return errors.New("PGbouncer enhanced health check failed: PGBouncer Database is not connected")
		}
		_, err = getUsers(ctx, db)
		if err != nil {
			return fmt.Errorf("PGbouncer enhanced health check failed: %w", err)
		}
	} else {
		err = probeLocalPort(ctx, config.PGBouncerPort)
		if err != nil {
			return fmt.Errorf("PGbouncer port probe check failed: %w", err)
		}
	}
	if config.CheckDDAgent {
		return getAgentHealth(ctx)
	}
	return nil
}

func probeLocalPort(ctx context.Context, port int) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf(":%d", port))
	if err == nil {
		conn.Close()
	}
	return err
}

func getAgentHealth(ctx context.Context) error {
	psCmd := exec.CommandContext(ctx, "sudo", "-n", "datadog-agent", "health")
	err := psCmd.Run()
	if err != nil {
		return fmt.Errorf("Datadog agent health command failed: %w", err)
	}
	return nil
}
