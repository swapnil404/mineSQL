FROM maven:3-eclipse-temurin-21 AS builder

WORKDIR /build
COPY pom.xml .
RUN mvn dependency:go-offline -B

COPY src ./src
RUN mvn package -DskipTests -q

FROM scratch AS artifact
COPY --from=builder /build/target/minesql-hal-0.1.0.jar /minesql-hal-0.1.0.jar
