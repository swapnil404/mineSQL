package dev.swapnil404.minesqlhal;

import org.bukkit.plugin.java.JavaPlugin;

import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.net.ServerSocket;
import java.net.Socket;
import java.net.SocketException;
import java.util.Queue;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.logging.Logger;

public class TCPServer {

    private final JavaPlugin plugin;
    private final Logger logger;
    private final int port;
    private volatile boolean running;
    private ServerSocket serverSocket;
    private Thread acceptThread;
    private final Queue<Socket> clientSockets;

    public TCPServer(JavaPlugin plugin, Logger logger, int port) {
        this.plugin = plugin;
        this.logger = logger;
        this.port = port;
        this.clientSockets = new ConcurrentLinkedQueue<>();
    }

    public void start() {
        if (running) return;

        try {
            serverSocket = new ServerSocket(port);
            running = true;
            acceptThread = new Thread(this::acceptLoop, "minesql-hal-accept");
            acceptThread.setDaemon(true);
            acceptThread.start();
            logger.info("mineSQL-HAL TCP server started on port " + port);
        } catch (IOException e) {
            logger.severe("Failed to start mineSQL-HAL TCP server: " + e.getMessage());
        }
    }

    public void stop() {
        running = false;

        try {
            if (serverSocket != null && !serverSocket.isClosed()) {
                serverSocket.close();
            }
        } catch (IOException e) {
            logger.warning("Error closing server socket: " + e.getMessage());
        }

        Socket socket;
        while ((socket = clientSockets.poll()) != null) {
            try {
                socket.close();
            } catch (IOException ignored) {
            }
        }

        if (acceptThread != null) {
            try {
                acceptThread.join(5000);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                acceptThread.interrupt();
            }
        }

        logger.info("mineSQL-HAL TCP server stopped");
    }

    private void acceptLoop() {
        while (running) {
            try {
                Socket socket = serverSocket.accept();
                clientSockets.add(socket);
                Thread handlerThread = new Thread(
                    () -> handleClient(socket),
                    "minesql-hal-client-" + socket.getRemoteSocketAddress()
                );
                handlerThread.setDaemon(true);
                handlerThread.start();
            } catch (SocketException e) {
                if (running) {
                    logger.warning("Server socket exception: " + e.getMessage());
                }
            } catch (IOException e) {
                if (running) {
                    logger.warning("Accept error: " + e.getMessage());
                }
            }
        }
    }

    private void handleClient(Socket socket) {
        try (socket) {
            DataInputStream in = new DataInputStream(socket.getInputStream());
            DataOutputStream out = new DataOutputStream(socket.getOutputStream());

            while (running) {
                int packetLength = in.readInt();
                if (packetLength < 1) {
                    break;
                }
                int opcode = in.readUnsignedByte();
                int payloadLength = packetLength - 1;
                byte[] payload = new byte[payloadLength];
                in.readFully(payload);

                route(opcode, payload, out);
            }
        } catch (SocketException e) {
            // connection closed (normal)
        } catch (IOException e) {
            if (running) {
                logger.warning("Client handler error: " + e.getMessage());
            }
        } finally {
            clientSockets.remove(socket);
        }
    }

    private void route(int opcode, byte[] payload, DataOutputStream out) {
        logger.info(String.format("Received opcode 0x%02X, payload length %d", opcode, payload.length));

        switch (opcode) {
            case 0x01:
                BlockHandler.handleWrite(plugin, payload, out);
                break;
            case 0x02:
                BlockHandler.handleRead(plugin, payload, out);
                break;
            case 0x03:
                BlockHandler.handleBatchRead(plugin, payload, out);
                break;
            case 0x04:
                BlockHandler.handleForceLoad(plugin, payload, out);
                break;
            case 0x05:
                BlockHandler.handleIsChunkLoaded(plugin, payload, out);
                break;
            case 0x06:
                BlockHandler.handleBatchWrite(plugin, payload, out);
                break;
            default:
                logger.warning(String.format("Unknown opcode 0x%02X", opcode));
        }
    }
}
