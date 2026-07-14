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

func main() {
	addr := flag.String("addr", "127.0.0.1:50612", "Airborne gRPC address")
	token := flag.String("token", "", "static bearer token")
	tenantID := flag.String("tenant", "ai8", "tenant ID")
	prompt := flag.String("prompt", "deterministic e2e", "user input")
	requestID := flag.String("request-id", "11111111-1111-4111-8111-111111111111", "stable request UUID")
	flag.Parse()

	if *token == "" {
		fatalf("token is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fatalf("dial: %v", err)
	}
	defer conn.Close()

	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+*token)
	response, err := pb.NewAirborneServiceClient(conn).GenerateReply(ctx, &pb.GenerateReplyRequest{
		TenantId:          *tenantID,
		Instructions:      "Reply with the deterministic fixture response.",
		UserInput:         *prompt,
		PreferredProvider: pb.Provider_PROVIDER_OPENAI,
		ClientId:          "e2e-grpc-probe",
		RequestId:         *requestID,
		ExternalRef:       "e2e-grpc-chat",
	})
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
