IMAGE="jdowni000/deployment-check"
TAG="dev"

build:
	docker build -t ${IMAGE}:${TAG} -f Dockerfile ../../

push:
	docker push ${IMAGE}:${TAG}