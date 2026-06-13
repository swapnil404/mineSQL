package dev.swapnil404.minesqlhal;

import org.bukkit.Bukkit;
import org.bukkit.Chunk;
import org.bukkit.DyeColor;
import org.bukkit.Material;
import org.bukkit.NamespacedKey;
import org.bukkit.World;
import org.bukkit.block.Banner;
import org.bukkit.block.Barrel;
import org.bukkit.block.Block;
import org.bukkit.block.BlockState;
import org.bukkit.block.Lectern;
import org.bukkit.block.Sign;
import org.bukkit.block.banner.Pattern;
import org.bukkit.block.banner.PatternType;
import org.bukkit.block.sign.Side;
import org.bukkit.inventory.ItemStack;
import org.bukkit.inventory.meta.BookMeta;
import org.bukkit.persistence.PersistentDataHolder;
import org.bukkit.persistence.PersistentDataType;
import org.bukkit.plugin.java.JavaPlugin;

import java.io.DataOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

public class BlockHandler {

    private static final NamespacedKey KEY = new NamespacedKey("minesql", "minesql_row");

    private static final byte BLOCK_TYPE_BARREL    = 0x00;
    private static final byte BLOCK_TYPE_BANNER    = 0x01;
    private static final byte BLOCK_TYPE_SIGN = 0x02;
    private static final byte BLOCK_TYPE_LECTERN   = 0x03;

    private static final PatternType[] PATTERN_TYPES = {
        PatternType.BASE,
        PatternType.STRIPE_BOTTOM,
        PatternType.STRIPE_TOP,
        PatternType.STRIPE_LEFT,
        PatternType.STRIPE_RIGHT,
        PatternType.STRIPE_CENTER,
        PatternType.STRIPE_MIDDLE,
        PatternType.STRIPE_DOWNRIGHT,
        PatternType.STRIPE_DOWNLEFT,
        PatternType.SMALL_STRIPES,
        PatternType.CROSS,
        PatternType.STRAIGHT_CROSS,
        PatternType.DIAGONAL_LEFT,
        PatternType.DIAGONAL_RIGHT,
        PatternType.DIAGONAL_UP_LEFT,
        PatternType.DIAGONAL_UP_RIGHT,
    };

    private static final DyeColor[] DYE_COLORS = DyeColor.values();

    public static void handleWrite(JavaPlugin plugin, byte[] payload, DataOutputStream out) {
        int offset = 0;
        int x = readInt32(payload, offset); offset += 4;
        int y = readInt32(payload, offset); offset += 4;
        int z = readInt32(payload, offset); offset += 4;
        byte blockType = payload[offset]; offset += 1;
        int dataLength = readUint32(payload, offset); offset += 4;
        String data = new String(payload, offset, dataLength, StandardCharsets.UTF_8);

        Bukkit.getScheduler().runTask(plugin, () -> {
            try {
                World world = Bukkit.getWorlds().get(0);
                Block block = world.getBlockAt(x, y, z);

                if (data.isEmpty()) {
                    block.setType(Material.AIR);
                    plugin.getLogger().info(String.format("WRITE AIR at (%d, %d, %d)", x, y, z));
                } else {
                    switch (blockType) {
                        case BLOCK_TYPE_BARREL:
                            placeBarrel(block, data);
                            break;
                        case BLOCK_TYPE_BANNER:
                            placeBanner(plugin, block, data);
                            break;
                        case BLOCK_TYPE_SIGN:
                            placeSign(plugin, block, data);
                            break;
                        case BLOCK_TYPE_LECTERN:
                            placeLectern(plugin, block, data);
                            break;
                        default:
                            throw new IllegalArgumentException("Unknown block type: 0x" + Integer.toHexString(blockType & 0xFF));
                    }
                    String preview = data.length() > 40 ? data.substring(0, 40) + "..." : data;
                    plugin.getLogger().info(String.format("WRITE OK at (%d, %d, %d) type=0x%02X: %s", x, y, z, blockType, preview));
                }

                writeAck(out);
            } catch (Exception e) {
                plugin.getLogger().warning("WRITE handler error: " + e.getMessage());
                try {
                    writeError(out, e.getMessage());
                } catch (IOException ignored) {
                }
            }
        });
    }

    public static void handleRead(JavaPlugin plugin, byte[] payload, DataOutputStream out) {
        int offset = 0;
        int x = readInt32(payload, offset); offset += 4;
        int y = readInt32(payload, offset); offset += 4;
        int z = readInt32(payload, offset);

        Bukkit.getScheduler().runTask(plugin, () -> {
            try {
                World world = Bukkit.getWorlds().get(0);
                Block block = world.getBlockAt(x, y, z);
                String data = readPDC(block);

                if (data != null && !data.isEmpty()) {
                    String preview = data.length() > 40 ? data.substring(0, 40) + "..." : data;
                    plugin.getLogger().info(String.format("READ HIT at (%d, %d, %d) type=%s: %s", x, y, z, block.getType(), preview));
                } else {
                    plugin.getLogger().info(String.format("READ MISS at (%d, %d, %d) type=%s", x, y, z, block.getType()));
                }

                writeData(out, data);
            } catch (Exception e) {
                plugin.getLogger().warning("READ handler error: " + e.getMessage());
                try {
                    writeError(out, e.getMessage());
                } catch (IOException ignored) {
                }
            }
        });
    }

    public static void handleBatchRead(JavaPlugin plugin, byte[] payload, DataOutputStream out) {
        int offset = 0;
        int count = readUint32(payload, offset);
        offset += 4;

        int[] xs = new int[count];
        int[] ys = new int[count];
        int[] zs = new int[count];
        for (int i = 0; i < count; i++) {
            xs[i] = readInt32(payload, offset); offset += 4;
            ys[i] = readInt32(payload, offset); offset += 4;
            zs[i] = readInt32(payload, offset); offset += 4;
        }

        Bukkit.getScheduler().runTask(plugin, () -> {
            try {
                World world = Bukkit.getWorlds().get(0);
                String[] results = new String[count];

                for (int i = 0; i < count; i++) {
                    Block block = world.getBlockAt(xs[i], ys[i], zs[i]);
                    String data = readPDC(block);
                    results[i] = (data != null && !data.isEmpty()) ? data : null;
                }

                writeBatchData(out, results);
            } catch (Exception e) {
                plugin.getLogger().warning("BATCH_READ handler error: " + e.getMessage());
                try {
                    writeError(out, e.getMessage());
                } catch (IOException ignored) {
                }
            }
        });
    }

    public static void handleBatchWrite(JavaPlugin plugin, byte[] payload, DataOutputStream out) {
        int offset = 0;
        int count = readUint32(payload, offset);
        offset += 4;

        int[] xs = new int[count];
        int[] ys = new int[count];
        int[] zs = new int[count];
        byte[] blockTypes = new byte[count];
        String[] datas = new String[count];
        for (int i = 0; i < count; i++) {
            xs[i] = readInt32(payload, offset); offset += 4;
            ys[i] = readInt32(payload, offset); offset += 4;
            zs[i] = readInt32(payload, offset); offset += 4;
            blockTypes[i] = payload[offset]; offset += 1;
            int dataLen = readUint32(payload, offset); offset += 4;
            datas[i] = new String(payload, offset, dataLen, StandardCharsets.UTF_8);
            offset += dataLen;
        }

        Bukkit.getScheduler().runTask(plugin, () -> {
            try {
                World world = Bukkit.getWorlds().get(0);

                for (int i = 0; i < count; i++) {
                    Block block = world.getBlockAt(xs[i], ys[i], zs[i]);

                    if (datas[i].isEmpty()) {
                        block.setType(Material.AIR);
                    } else {
                        switch (blockTypes[i]) {
                            case BLOCK_TYPE_BARREL:
                                placeBarrel(block, datas[i]);
                                break;
                            case BLOCK_TYPE_BANNER:
                                placeBanner(plugin, block, datas[i]);
                                break;
                            case BLOCK_TYPE_SIGN:
                                placeSign(plugin, block, datas[i]);
                                break;
                            case BLOCK_TYPE_LECTERN:
                                placeLectern(plugin, block, datas[i]);
                                break;
                            default:
                                throw new IllegalArgumentException("Unknown block type: 0x" + Integer.toHexString(blockTypes[i] & 0xFF));
                        }
                    }
                }

                writeAck(out);
            } catch (Exception e) {
                plugin.getLogger().warning("BATCH_WRITE handler error: " + e.getMessage());
                try {
                    writeError(out, e.getMessage());
                } catch (IOException ignored) {
                }
            }
        });
    }

    public static void handleForceLoad(JavaPlugin plugin, byte[] payload, DataOutputStream out) {
        int chunkX = readInt32(payload, 0);
        int chunkZ = readInt32(payload, 4);

        World world = Bukkit.getWorlds().get(0);
        world.getChunkAtAsync(chunkX, chunkZ, true).thenAccept(chunk -> {
            chunk.addPluginChunkTicket(plugin);
            Bukkit.getScheduler().runTask(plugin, () -> {
                try {
                    writeAck(out);
                } catch (IOException e) {
                    plugin.getLogger().warning("Failed to send ACK: " + e.getMessage());
                }
            });
        });
    }

    public static void handleIsChunkLoaded(JavaPlugin plugin, byte[] payload, DataOutputStream out) {
        int chunkX = readInt32(payload, 0);
        int chunkZ = readInt32(payload, 4);

        Bukkit.getScheduler().runTask(plugin, () -> {
            try {
                World world = Bukkit.getWorlds().get(0);
                boolean loaded = world.isChunkLoaded(chunkX, chunkZ);

                byte[] dataBytes = new byte[] { (byte) (loaded ? 0x01 : 0x00) };
                out.writeInt(6);
                out.writeByte(0x11);
                out.writeInt(1);
                out.write(dataBytes);
                out.flush();
            } catch (Exception e) {
                plugin.getLogger().warning("IS_CHUNK_LOADED handler error: " + e.getMessage());
                try {
                    writeError(out, e.getMessage());
                } catch (IOException ignored) {
                }
            }
        });
    }

    private static void placeBarrel(Block block, String data) {
        block.setType(Material.BARREL);
        Barrel barrel = (Barrel) block.getState();
        barrel.getPersistentDataContainer().set(KEY, PersistentDataType.STRING, data);
        barrel.update();
    }

    private static void placeBanner(JavaPlugin plugin, Block block, String hexData) {
        block.setType(Material.WHITE_BANNER);
        BlockState state = block.getState();
        if (state == null) {
            return;
        }
        if (state instanceof PersistentDataHolder holder) {
            holder.getPersistentDataContainer().set(KEY, PersistentDataType.STRING, hexData);
        }
        if (state instanceof Banner banner && hexData.length() == 12) {
            setBannerPatterns(banner, hexData);
        } else {
            plugin.getLogger().warning("Banner state not available for pattern setting; PDC data stored");
        }
        state.update(true);
    }

    private static void setBannerPatterns(Banner banner, String hexData) {
        if (hexData.length() != 12) {
            throw new IllegalArgumentException("Banner data must be exactly 12 hex characters (6 bytes)");
        }
        List<Pattern> patterns = new ArrayList<>(6);
        for (int i = 0; i < 6; i++) {
            int byteVal = Integer.parseInt(hexData.substring(i * 2, i * 2 + 2), 16);
            int patternIdx = (byteVal >> 4) & 0x0F;
            int dyeIdx = byteVal & 0x0F;
            patterns.add(new Pattern(DYE_COLORS[dyeIdx], PATTERN_TYPES[patternIdx]));
        }
        banner.setPatterns(patterns);
    }

    private static void placeSign(JavaPlugin plugin, Block block, String data) {
        block.setType(Material.OAK_SIGN);
        BlockState state = block.getState();
        if (state == null) {
            return;
        }
        if (state instanceof PersistentDataHolder holder) {
            holder.getPersistentDataContainer().set(KEY, PersistentDataType.STRING, data);
        }
        if (state instanceof Sign sign) {
            String[] lines = data.split("\0", -1);
            for (int i = 0; i < 4; i++) {
                String line = (i < lines.length) ? lines[i] : "";
                if (line.length() > 16) {
                    line = line.substring(0, 16);
                }
                sign.getSide(Side.FRONT).setLine(i, line);
            }
        } else {
            plugin.getLogger().warning("Sign state not available for line setting; PDC data stored");
        }
        state.update(true);
    }

    private static void placeLectern(JavaPlugin plugin, Block block, String data) {
        block.setType(Material.LECTERN);
        Lectern lectern = (Lectern) block.getState();
        if (lectern == null) {
            return;
        }
        lectern.getPersistentDataContainer().set(KEY, PersistentDataType.STRING, data);

        String[] rawPages = data.split("\n---\n");
        List<String> pages = new ArrayList<>();
        for (String page : rawPages) {
            if (!page.isEmpty()) {
                pages.add(page);
            }
        }
        if (pages.isEmpty()) {
            pages.add(data);
        }
        plugin.getLogger().info(String.format("LECTERN book: %d pages at (%d, %d, %d)", pages.size(), block.getX(), block.getY(), block.getZ()));

        ItemStack book = new ItemStack(Material.WRITTEN_BOOK);
        BookMeta meta = (BookMeta) book.getItemMeta();
        meta.setTitle("WAL Entry");
        meta.setAuthor("mineSQL");
        for (String page : pages) {
            meta.addPage(page);
        }
        book.setItemMeta(meta);
        lectern.getSnapshotInventory().setItem(0, book);
        lectern.update(true, true);

        String blockDataStr = block.getBlockData().getAsString();
        String updatedBlockData = blockDataStr.replace("has_book=false", "has_book=true");
        if (!updatedBlockData.equals(blockDataStr)) {
            block.setBlockData(Bukkit.createBlockData(updatedBlockData));
        }
    }

    private static String readPDC(Block block) {
        if (block.getState() instanceof PersistentDataHolder holder) {
            return holder.getPersistentDataContainer().get(KEY, PersistentDataType.STRING);
        }
        return null;
    }

    private static void writeData(DataOutputStream out, String data) throws IOException {
        if (data == null || data.isEmpty()) {
            out.writeInt(5);
            out.writeByte(0x11);
            out.writeInt(0);
        } else {
            byte[] dataBytes = data.getBytes(StandardCharsets.UTF_8);
            out.writeInt(5 + dataBytes.length);
            out.writeByte(0x11);
            out.writeInt(dataBytes.length);
            out.write(dataBytes);
        }
        out.flush();
    }

    private static void writeBatchData(DataOutputStream out, String[] results) throws IOException {
        int packetLen = 5;
        byte[][] dataArrays = new byte[results.length][];
        for (int i = 0; i < results.length; i++) {
            if (results[i] != null) {
                dataArrays[i] = results[i].getBytes(StandardCharsets.UTF_8);
                packetLen += 4 + dataArrays[i].length;
            } else {
                dataArrays[i] = new byte[0];
                packetLen += 4;
            }
        }

        out.writeInt(packetLen);
        out.writeByte(0x12);
        out.writeInt(results.length);

        for (byte[] data : dataArrays) {
            out.writeInt(data.length);
            if (data.length > 0) {
                out.write(data);
            }
        }
        out.flush();
    }

    private static void writeError(DataOutputStream out, String message) throws IOException {
        byte[] msgBytes = message.getBytes(StandardCharsets.UTF_8);
        out.writeInt(5 + msgBytes.length);
        out.writeByte(0xFF);
        out.writeInt(msgBytes.length);
        out.write(msgBytes);
        out.flush();
    }

    private static void writeAck(DataOutputStream out) throws IOException {
        out.writeInt(1);
        out.writeByte(0x10);
        out.flush();
    }

    private static int readInt32(byte[] buf, int offset) {
        return (buf[offset] << 24)
             | ((buf[offset + 1] & 0xFF) << 16)
             | ((buf[offset + 2] & 0xFF) << 8)
             | (buf[offset + 3] & 0xFF);
    }

    private static int readUint32(byte[] buf, int offset) {
        return ((buf[offset] & 0xFF) << 24)
             | ((buf[offset + 1] & 0xFF) << 16)
             | ((buf[offset + 2] & 0xFF) << 8)
             | (buf[offset + 3] & 0xFF);
    }
}
