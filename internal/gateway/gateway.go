package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/footprintai/containarium/internal/auth"
	"github.com/footprintai/containarium/internal/mtls"
	pb "github.com/footprintai/containarium/pkg/pb/containarium/v1"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/rs/cors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
)

// GatewayServer implements the HTTP/REST gateway for the gRPC service
type GatewayServer struct {
	grpcAddress    string
	httpPort       int
	authMiddleware *auth.AuthMiddleware
	swaggerDir     string
	certsDir       string // Optional: for mTLS connection to gRPC server
}

// NewGatewayServer creates a new gateway server
func NewGatewayServer(grpcAddress string, httpPort int, authMiddleware *auth.AuthMiddleware, swaggerDir string, certsDir string) *GatewayServer {
	return &GatewayServer{
		grpcAddress:    grpcAddress,
		httpPort:       httpPort,
		authMiddleware: authMiddleware,
		swaggerDir:     swaggerDir,
		certsDir:       certsDir,
	}
}

// Start starts the HTTP gateway server
func (gs *GatewayServer) Start(ctx context.Context) error {
	// Create grpc-gateway mux with custom options
	mux := runtime.NewServeMux(
		runtime.WithErrorHandler(customErrorHandler),
		runtime.WithMetadata(annotateContext),
		// Marshal options for better JSON formatting
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames:   false,
				EmitUnpopulated: true,
				UseEnumNumbers:  false,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				DiscardUnknown: true,
			},
		}),
	)

	// Setup connection to gRPC server
	var opts []grpc.DialOption
	if gs.certsDir != "" {
		// Use mTLS to connect to gRPC server
		certPaths := mtls.CertPathsFromDir(gs.certsDir)
		dialOpts, err := mtls.LoadClientDialOptions(certPaths, gs.grpcAddress)
		if err != nil {
			return fmt.Errorf("failed to load mTLS credentials for gateway: %w", err)
		}
		opts = dialOpts
		log.Printf("Gateway connecting to gRPC server with mTLS")
	} else {
		// Use insecure connection (local development)
		opts = []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}
		log.Printf("Gateway connecting to gRPC server without TLS")
	}

	// Register gateway handlers
	if err := pb.RegisterContainerServiceHandlerFromEndpoint(ctx, mux, gs.grpcAddress, opts); err != nil {
		return fmt.Errorf("failed to register gateway: %w", err)
	}

	// Create HTTP handler with authentication middleware
	handler := gs.authMiddleware.HTTPMiddleware(mux)

	// Add CORS support
	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{"*"}, // Configure appropriately for production
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		ExposedHeaders: []string{"Content-Length"},
		MaxAge:         300,
	}).Handler(handler)

	// Create HTTP mux for routing
	httpMux := http.NewServeMux()

	// API routes (with authentication)
	httpMux.Handle("/v1/", corsHandler)

	// Swagger UI routes (no authentication required)
	httpMux.Handle("/swagger-ui/", http.StripPrefix("/swagger-ui", http.HandlerFunc(ServeSwaggerUI(gs.swaggerDir))))
	httpMux.HandleFunc("/swagger.json", ServeSwaggerSpec(gs.swaggerDir))

	// Health check endpoint (no auth required)
	httpMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	// Start HTTP server
	addr := fmt.Sprintf(":%d", gs.httpPort)
	log.Printf("Starting HTTP/REST gateway on %s", addr)
	log.Printf("Swagger UI available at http://localhost%s/swagger-ui/", addr)
	log.Printf("OpenAPI spec available at http://localhost%s/swagger.json", addr)

	return http.ListenAndServe(addr, httpMux)
}

// customErrorHandler provides better error messages
func customErrorHandler(ctx context.Context, mux *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	w.Header().Set("Content-Type", "application/json")

	// Map gRPC codes to HTTP status codes
	httpStatus := runtime.HTTPStatusFromCode(grpc.Code(err))
	w.WriteHeader(httpStatus)

	// Format error response
	errorResp := map[string]interface{}{
		"error": err.Error(),
		"code":  httpStatus,
	}

	json.NewEncoder(w).Encode(errorResp)
}

// annotateContext adds metadata to context
func annotateContext(ctx context.Context, req *http.Request) metadata.MD {
	return metadata.Pairs(
		"x-forwarded-method", req.Method,
		"x-forwarded-path", req.URL.Path,
	)
}
