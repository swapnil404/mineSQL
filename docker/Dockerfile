FROM eclipse-temurin:21-jre

WORKDIR /minecraft

ARG PAPER_VERSION=1.21
ARG PAPER_BUILD=130

RUN apt-get update && apt-get install -y wget jq && rm -rf /var/lib/apt/lists/*

RUN wget -q "https://api.papermc.io/v2/projects/paper/versions/${PAPER_VERSION}/builds/${PAPER_BUILD}/downloads/paper-${PAPER_VERSION}-${PAPER_BUILD}.jar" -O paper.jar

RUN mkdir -p plugins/mineSQL-HAL

COPY minesql-hal.jar plugins/
COPY plugins/mineSQL-HAL/config.yml plugins/mineSQL-HAL/

RUN echo "eula=true" > eula.txt

COPY server.properties server.properties

VOLUME ["/minecraft/world", "/minecraft/logs"]

COPY start.sh /start.sh
RUN chmod +x /start.sh

EXPOSE 25565 25576

CMD ["/start.sh"]
