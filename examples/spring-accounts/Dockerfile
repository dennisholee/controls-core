FROM alpine:3.20
RUN apk add --no-cache openjdk21-jre-headless
COPY target/spring-accounts-0.1.0.jar /app.jar
ENTRYPOINT ["java", "-jar", "/app.jar"]
