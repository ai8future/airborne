// Command probe performs a real authenticated Airborne gRPC chat for E2E.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const operationTimeout = 120 * time.Second

type probeOptions struct {
	addr           string
	token          string
	tenantID       string
	prompt         string
	requestID      string
	idempotencyKey string
}

func parseOptions(args []string) (probeOptions, error) {
	var options probeOptions
	flags := flag.NewFlagSet("airborne-e2e-probe", flag.ContinueOnError)
	flags.StringVar(&options.addr, "addr", "127.0.0.1:50612", "Airborne gRPC address")
	flags.StringVar(&options.token, "token", "", "static bearer token")
	flags.StringVar(&options.tenantID, "tenant", "ai8", "tenant ID")
	flags.StringVar(&options.prompt, "prompt", "deterministic e2e", "user input")
	flags.StringVar(&options.requestID, "request-id", "11111111-1111-4111-8111-111111111111", "stable request UUID")
	flags.StringVar(&options.idempotencyKey, "idempotency-key", "", "stable replay idempotency key")
	if err := flags.Parse(args); err != nil {
		return probeOptions{}, err
	}
	if options.token == "" {
		return probeOptions{}, fmt.Errorf("token is required")
	}
	if options.idempotencyKey == "" {
		return probeOptions{}, fmt.Errorf("idempotency-key is required")
	}
	return options, nil
}

func newGenerateReplyRequest(options probeOptions) *pb.GenerateReplyRequest {
	return &pb.GenerateReplyRequest{
		TenantId:          options.tenantID,
		Instructions:      "Reply with the deterministic fixture response.",
		UserInput:         options.prompt,
		PreferredProvider: pb.Provider_PROVIDER_OPENAI,
		ClientId:          "e2e-grpc-probe",
		RequestId:         options.requestID,
		IdempotencyKey:    options.idempotencyKey,
		ExternalRef:       "e2e-grpc-chat",
	}
}

func main() {
	options, err := parseOptions(os.Args[1:])
	if err != nil {
		fatalf("%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	conn, err := grpc.NewClient(options.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fatalf("dial: %v", err)
	}
	defer conn.Close()

	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+options.token)
	response, err := pb.NewAirborneServiceClient(conn).GenerateReply(ctx, newGenerateReplyRequest(options))
	if err != nil {
		fatalf("GenerateReply: %v", err)
	}

	result := struct {
		Text     string `json:"text"`
		Model    string `json:"model"`
		Provider string `json:"provider"`
	}{
		Text:     response.GetText(),
		Model:    response.GetModel(),
		Provider: response.GetProvider().String(),
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fatalf("encode response: %v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
