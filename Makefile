.PHONY: all build run dev docker-build docker-run

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

docker-build:
	docker build -t journal-app .

docker-run:
	docker run -p 8080:8080 \
		-e JOURNAL_PASSWORD \
		-e GEMINI_API_TOKEN \
		-e GIT_USERNAME \
		-e GIT_REPO_NAME \
		-e GITHUB_TOKEN \
		journal-app
