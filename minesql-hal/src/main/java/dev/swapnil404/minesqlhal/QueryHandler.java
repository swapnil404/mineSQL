package dev.swapnil404.minesqlhal;

import com.google.gson.Gson;
import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import net.kyori.adventure.text.Component;
import net.kyori.adventure.text.format.NamedTextColor;
import org.bukkit.Bukkit;
import org.bukkit.command.Command;
import org.bukkit.command.CommandExecutor;
import org.bukkit.command.CommandSender;
import org.bukkit.entity.Player;
import org.bukkit.plugin.java.JavaPlugin;

import java.io.BufferedReader;
import java.io.BufferedWriter;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.OutputStreamWriter;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.nio.charset.StandardCharsets;

public class QueryHandler implements CommandExecutor {

    private static final Gson GSON = new Gson();
    private final JavaPlugin plugin;
    private final String host;
    private final int port;

    public QueryHandler(JavaPlugin plugin, String host, int port) {
        this.plugin = plugin;
        this.host = host;
        this.port = port;
    }

    @Override
    public boolean onCommand(CommandSender sender, Command command, String label, String[] args) {
        plugin.getLogger().info("SQL command received: " + String.join(" ", args));

        if (!(sender instanceof Player player)) {
            sender.sendMessage("This command can only be run by a player.");
            return true;
        }

        String query = String.join(" ", args).trim();
        if (query.isEmpty()) {
            player.sendMessage(Component.text("Usage: /sql <query>", NamedTextColor.RED));
            return true;
        }

        Bukkit.getScheduler().runTaskAsynchronously(plugin, () -> {
            try {
                Socket socket = new Socket();
                socket.connect(new InetSocketAddress(host, port), 5000);
                socket.setSoTimeout(30000);
                try (socket) {
                    BufferedWriter out = new BufferedWriter(
                            new OutputStreamWriter(socket.getOutputStream(), StandardCharsets.UTF_8));
                    BufferedReader in = new BufferedReader(
                            new InputStreamReader(socket.getInputStream(), StandardCharsets.UTF_8));

                    out.write(query + "\n");
                    out.flush();

                    String line = in.readLine();
                    if (line == null) {
                        throw new IOException("query endpoint closed connection without response");
                    }

                    plugin.getLogger().info("SQL query response received: " + line);

                    JsonObject resp = GSON.fromJson(line, JsonObject.class);

                    Bukkit.getScheduler().runTask(plugin, () -> {
                        if (resp.has("error")) {
                            player.sendMessage(Component.text(resp.get("error").getAsString(), NamedTextColor.RED));
                        } else {
                            renderResult(player, resp);
                        }
                    });
                }
            } catch (IOException e) {
                plugin.getLogger().warning("SQL query IO error: " + e.getClass().getSimpleName() + ": " + e.getMessage());
                Bukkit.getScheduler().runTask(plugin, () -> {
                    player.sendMessage(Component.text("Failed to connect to mineSQL query endpoint: " + e.getMessage(), NamedTextColor.RED));
                });
            } catch (Exception e) {
                plugin.getLogger().severe("SQL query unexpected error: " + e.getClass().getSimpleName() + ": " + e.getMessage());
                Bukkit.getScheduler().runTask(plugin, () -> {
                    player.sendMessage(Component.text("Unexpected error: " + e.getMessage(), NamedTextColor.RED));
                });
            }
        });

        return true;
    }

    private void renderResult(Player player, JsonObject result) {
        if (result.has("columns") && result.has("rows")) {
            JsonArray columns = result.getAsJsonArray("columns");
            if (columns.isEmpty()) {
                player.sendMessage(Component.text(result.toString(), NamedTextColor.GREEN));
                return;
            }
            renderTable(player, result);
            return;
        }

        player.sendMessage(Component.text(result.toString()));
    }

    private void renderTable(Player player, JsonObject result) {
        JsonArray columns = result.getAsJsonArray("columns");
        JsonArray rows = result.getAsJsonArray("rows");
        int rowCount = result.has("row_count") ? result.get("row_count").getAsInt() : rows.size();
        boolean truncated = result.has("truncated") && result.get("truncated").getAsBoolean();

        int maxDisplayRows = 8;
        int displayRows = truncated ? Math.min(rows.size(), maxDisplayRows) : rows.size();

        int[] colWidths = new int[columns.size()];
        for (int i = 0; i < columns.size(); i++) {
            colWidths[i] = Math.max(columns.get(i).getAsString().length(), 4);
        }
        for (int r = 0; r < displayRows; r++) {
            JsonArray row = rows.get(r).getAsJsonArray();
            for (int c = 0; c < row.size() && c < columns.size(); c++) {
                int len = cellString(row.get(c)).length();
                if (len > colWidths[c]) colWidths[c] = len;
            }
        }

        StringBuilder headerLine = new StringBuilder();
        for (int i = 0; i < columns.size(); i++) {
            headerLine.append(padRight(columns.get(i).getAsString(), colWidths[i]));
            if (i < columns.size() - 1) headerLine.append(" | ");
        }
        player.sendMessage(Component.text(headerLine.toString(), NamedTextColor.GOLD));

        StringBuilder sepLine = new StringBuilder();
        for (int i = 0; i < columns.size(); i++) {
            sepLine.append("-".repeat(colWidths[i]));
            if (i < columns.size() - 1) sepLine.append("-+-");
        }
        player.sendMessage(Component.text(sepLine.toString(), NamedTextColor.GRAY));

        for (int r = 0; r < displayRows; r++) {
            JsonArray row = rows.get(r).getAsJsonArray();
            StringBuilder rowStr = new StringBuilder();
            for (int c = 0; c < columns.size(); c++) {
                String val = c < row.size() ? cellString(row.get(c)) : "";
                rowStr.append(padRight(val, colWidths[c]));
                if (c < columns.size() - 1) rowStr.append(" | ");
            }
            NamedTextColor color = (r % 2 == 0) ? NamedTextColor.WHITE : NamedTextColor.GRAY;
            player.sendMessage(Component.text(rowStr.toString(), color));
        }

        if (truncated && rows.size() > maxDisplayRows) {
            int remaining = rowCount - maxDisplayRows;
            player.sendMessage(Component.text("... " + remaining + " more rows", NamedTextColor.YELLOW));
        }

        player.sendMessage(Component.text("(" + rowCount + " row" + (rowCount == 1 ? "" : "s") + ")", NamedTextColor.GRAY));
    }

    private String cellString(JsonElement element) {
        if (element == null || element.isJsonNull()) return "null";
        if (element.isJsonPrimitive()) return element.getAsString();
        return element.toString();
    }

    private String padRight(String s, int n) {
        return String.format("%-" + n + "s", s);
    }
}
