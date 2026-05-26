.PHONY: proto

PROTO_DIR := proto
GEN_DIR := gen

proto:
	protoc -I $(PROTO_DIR) \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/phrony/runtime/v1/*.proto $(PROTO_DIR)/grpc/health/v1/*.proto
