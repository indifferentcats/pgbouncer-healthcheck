package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/julienschmidt/httprouter"
)

type requestHandlerWithContext func(context.Context, http.ResponseWriter, httprouter.Params) error
type requestHandler func(http.ResponseWriter, httprouter.Params) error

func makeRequestHandlerWithContext(h requestHandlerWithContext) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		err := h(ctx, w, ps)
		if err != nil {
			errMsg := fmt.Sprintf("An error occurred: %s", err)
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
	}
}

func makeRequestHandler(h requestHandler) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		err := h(w, ps)
		if err != nil {
			errMsg := fmt.Sprintf("An error occurred: %s", err)
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
	}
}

func makeRequestHandlerCommand(desc string, cmdName string, cmdArg ...string) httprouter.Handle {
	return makeRequestHandlerWithContext(func(ctx context.Context, w http.ResponseWriter, ps httprouter.Params) error {
		cmd := exec.CommandContext(ctx, cmdName, cmdArg...)
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("Error fetching %s: %w", desc, err)
		}
		if _, err := w.Write(output); err != nil {
			log.Printf("Error writing %s response: %s", desc, err)
		}
		return nil
	})
}

func makeRequestHandlerFile(desc string, path string) httprouter.Handle {
	return makeRequestHandler(func(w http.ResponseWriter, ps httprouter.Params) error {
		output, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("Error fetching %s: %w", desc, err)
		}
		if _, err := w.Write(output); err != nil {
			log.Printf("Error writing %s response: %s", desc, err)
		}
		return nil
	})
}

func returnJSON(w http.ResponseWriter, item interface{}) error {

	resp, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("Error converting response to JSON: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(resp); err != nil {
		log.Printf("Error writing JSON response: %s", err)
	}
	return nil
}
