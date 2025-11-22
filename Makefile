.PHONY: all build run dev

all: build

build:
	cd frontend && npm install && npm run build
	go build -o journal main.go

run:
	./journal

dev:
	# Run the Go backend in the background
	go run main.go & \
	# Run the React frontend
	cd frontend && npm start
