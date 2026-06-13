package dev.swapnil404.minesqlhal;

import org.bukkit.plugin.java.JavaPlugin;

public final class Main extends JavaPlugin {

    private TCPServer tcpServer;

    @Override
    public void onEnable() {
        saveDefaultConfig();
        String host = getConfig().getString("minesql-host", System.getenv("MINESQL_HOST"));
        if (host == null || host.isEmpty()) {
            host = "172.17.0.1";
        }
        int queryPort = getConfig().getInt("minesql-query-port", 5456);

        tcpServer = new TCPServer(this, getLogger(), 25576);
        tcpServer.start();

        getCommand("sql").setExecutor(new QueryHandler(this, host, queryPort));
        getLogger().info("SQL command handler registered: " + (getCommand("sql") != null));

        getLogger().info("mineSQL-HAL v" + getPluginMeta().getVersion() + " enabled (engine: " + host + ":" + queryPort + ")");
    }

    @Override
    public void onDisable() {
        if (tcpServer != null) {
            tcpServer.stop();
        }
        getLogger().info("mineSQL-HAL disabled");
    }
}
