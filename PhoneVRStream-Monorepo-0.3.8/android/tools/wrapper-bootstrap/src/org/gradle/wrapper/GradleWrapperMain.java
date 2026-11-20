package org.gradle.wrapper;

import java.io.BufferedInputStream;
import java.io.BufferedOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.util.Enumeration;
import java.util.Properties;
import java.util.zip.ZipEntry;
import java.util.zip.ZipFile;

/**
 * Small project-local Gradle bootstrapper.
 *
 * It intentionally implements only the subset needed by this project:
 * read gradle-wrapper.properties, download a pinned Gradle distribution,
 * verify its SHA-256, unpack it under GRADLE_USER_HOME, and delegate all
 * command-line arguments to the downloaded Gradle executable.
 *
 * The class name matches the Gradle Wrapper entry point so Android Studio
 * and the supplied gradlew scripts can invoke it without a global Gradle.
 */
public final class GradleWrapperMain {
    private static final int CONNECT_TIMEOUT_MS = 30000;
    private static final int READ_TIMEOUT_MS = 120000;

    private GradleWrapperMain() { }

    public static void main(String[] args) {
        int code;
        try {
            code = run(args);
        } catch (Exception ex) {
            System.err.println("PhoneVRStream Gradle bootstrap failed: " + ex.getMessage());
            ex.printStackTrace(System.err);
            code = 1;
        }
        if (code != 0) {
            System.exit(code);
        }
    }

    private static int run(String[] args) throws Exception {
        Path projectDir = new File(System.getProperty("user.dir")).toPath().toAbsolutePath().normalize();
        Path propertiesFile = projectDir.resolve("gradle/wrapper/gradle-wrapper.properties");
        if (!Files.isRegularFile(propertiesFile)) {
            throw new IOException("Cannot find " + propertiesFile + ". Run this command from the project root.");
        }

        Properties properties = new Properties();
        try (InputStream in = Files.newInputStream(propertiesFile)) {
            properties.load(in);
        }

        String distributionUrl = required(properties, "distributionUrl");
        String expectedSha256 = properties.getProperty("distributionSha256Sum", "").trim().toLowerCase();
        String fileName = distributionFileName(distributionUrl);
        String distributionName = fileName.endsWith(".zip") ? fileName.substring(0, fileName.length() - 4) : fileName;

        Path gradleUserHome = gradleUserHome();
        Path installRoot = gradleUserHome.resolve("wrapper/dists/phonvrstream/" + distributionName);
        Files.createDirectories(installRoot);

        Path gradleHome = locateGradleHome(installRoot, distributionName);
        if (gradleHome == null) {
            Path zipFile = installRoot.resolve(fileName);
            if (!Files.isRegularFile(zipFile) || !matchesSha256(zipFile, expectedSha256)) {
                Files.deleteIfExists(zipFile);
                Path temporary = installRoot.resolve(fileName + ".part");
                Files.deleteIfExists(temporary);
                System.out.println("Downloading Gradle distribution: " + distributionUrl);
                download(distributionUrl, temporary);
                if (!matchesSha256(temporary, expectedSha256)) {
                    Files.deleteIfExists(temporary);
                    throw new IOException("Gradle distribution SHA-256 verification failed. The download was discarded.");
                }
                move(temporary, zipFile);
            }

            System.out.println("Installing Gradle distribution...");
            extractZip(zipFile, installRoot);
            gradleHome = locateGradleHome(installRoot, distributionName);
            if (gradleHome == null) {
                throw new IOException("Gradle distribution was unpacked, but its bin/gradle executable was not found.");
            }
        }

        boolean windows = isWindows();
        Path executable = gradleHome.resolve(windows ? "bin/gradle.bat" : "bin/gradle");
        if (!Files.isRegularFile(executable)) {
            throw new IOException("Gradle executable is missing: " + executable);
        }
        if (!windows && !executable.toFile().canExecute() && !executable.toFile().setExecutable(true)) {
            throw new IOException("Cannot mark Gradle executable as executable: " + executable);
        }

        ProcessBuilder builder;
        if (windows) {
            StringBuilder command = new StringBuilder(quoteForCmd(executable.toString()));
            for (String arg : args) {
                command.append(' ').append(quoteForCmd(arg));
            }
            // cmd.exe /c treats a leading quoted executable specially and can
            // merge the executable path with the first argument. With /s, an
            // extra pair of outer quotes is required so cmd strips only that
            // pair and preserves the inner executable/argument quotes.
            String cmdCommand = "\"" + command.toString() + "\"";
            builder = new ProcessBuilder("cmd.exe", "/d", "/s", "/c", cmdCommand);
        } else {
            java.util.List<String> command = new java.util.ArrayList<String>();
            command.add(executable.toString());
            for (String arg : args) command.add(arg);
            builder = new ProcessBuilder(command);
        }
        builder.directory(projectDir.toFile());
        builder.inheritIO();
        builder.environment().putIfAbsent("GRADLE_USER_HOME", gradleUserHome.toString());
        return builder.start().waitFor();
    }

    private static String required(Properties properties, String key) throws IOException {
        String value = properties.getProperty(key);
        if (value == null || value.trim().isEmpty()) throw new IOException("Missing " + key + " in gradle-wrapper.properties");
        return value.trim();
    }

    private static Path gradleUserHome() {
        String configured = System.getenv("GRADLE_USER_HOME");
        if (configured != null && !configured.trim().isEmpty()) return new File(configured).toPath().toAbsolutePath();
        return new File(System.getProperty("user.home"), ".gradle").toPath().toAbsolutePath();
    }

    private static String distributionFileName(String url) throws IOException {
        String normalized = url;
        int query = normalized.indexOf('?');
        if (query >= 0) normalized = normalized.substring(0, query);
        int slash = normalized.lastIndexOf('/');
        if (slash < 0 || slash == normalized.length() - 1) throw new IOException("Invalid distributionUrl: " + url);
        return normalized.substring(slash + 1);
    }

    private static Path locateGradleHome(Path installRoot, String distributionName) throws IOException {
        Path expected = installRoot.resolve(distributionName);
        if (hasGradleExecutable(expected)) return expected;
        try (java.util.stream.Stream<Path> paths = Files.list(installRoot)) {
            return paths.filter(Files::isDirectory).filter(GradleWrapperMain::hasGradleExecutable).findFirst().orElse(null);
        }
    }

    private static boolean hasGradleExecutable(Path dir) {
        return Files.isRegularFile(dir.resolve("bin/gradle")) || Files.isRegularFile(dir.resolve("bin/gradle.bat"));
    }

    private static void download(String urlString, Path target) throws IOException {
        URL url = new URL(urlString);
        HttpURLConnection connection = (HttpURLConnection) url.openConnection();
        connection.setInstanceFollowRedirects(true);
        connection.setConnectTimeout(CONNECT_TIMEOUT_MS);
        connection.setReadTimeout(READ_TIMEOUT_MS);
        connection.setRequestProperty("User-Agent", "PhoneVRStream-GradleBootstrap/0.1.2");
        int status = connection.getResponseCode();
        if (status < 200 || status >= 300) throw new IOException("Download returned HTTP " + status + " for " + urlString);
        long total = connection.getContentLengthLong();
        long copied = 0L;
        try (InputStream in = new BufferedInputStream(connection.getInputStream());
             OutputStream out = new BufferedOutputStream(new FileOutputStream(target.toFile()))) {
            byte[] buffer = new byte[1024 * 128];
            int count;
            while ((count = in.read(buffer)) != -1) {
                out.write(buffer, 0, count);
                copied += count;
                if (total > 0 && copied % (32L * 1024L * 1024L) < buffer.length) {
                    System.out.println("Downloaded " + (copied / (1024 * 1024)) + " / " + (total / (1024 * 1024)) + " MB");
                }
            }
        } finally {
            connection.disconnect();
        }
    }

    private static boolean matchesSha256(Path file, String expected) throws Exception {
        if (expected.isEmpty()) return Files.isRegularFile(file);
        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        try (InputStream in = new BufferedInputStream(new FileInputStream(file.toFile()))) {
            byte[] buffer = new byte[1024 * 128];
            int count;
            while ((count = in.read(buffer)) != -1) digest.update(buffer, 0, count);
        }
        StringBuilder actual = new StringBuilder();
        for (byte b : digest.digest()) actual.append(String.format("%02x", b & 0xff));
        return expected.equals(actual.toString());
    }

    private static void extractZip(Path zipPath, Path destination) throws IOException {
        String base = destination.toAbsolutePath().normalize().toString() + File.separator;
        try (ZipFile zip = new ZipFile(zipPath.toFile())) {
            Enumeration<? extends ZipEntry> entries = zip.entries();
            while (entries.hasMoreElements()) {
                ZipEntry entry = entries.nextElement();
                Path output = destination.resolve(entry.getName()).toAbsolutePath().normalize();
                if (!output.toString().startsWith(base)) throw new IOException("Unsafe path in Gradle distribution: " + entry.getName());
                if (entry.isDirectory()) {
                    Files.createDirectories(output);
                } else {
                    Path parent = output.getParent();
                    if (parent != null) Files.createDirectories(parent);
                    try (InputStream in = new BufferedInputStream(zip.getInputStream(entry));
                         OutputStream out = new BufferedOutputStream(new FileOutputStream(output.toFile()))) {
                        byte[] buffer = new byte[65536];
                        int count;
                        while ((count = in.read(buffer)) != -1) out.write(buffer, 0, count);
                    }
                }
            }
        }
    }

    private static void move(Path source, Path destination) throws IOException {
        try {
            Files.move(source, destination, java.nio.file.StandardCopyOption.REPLACE_EXISTING, java.nio.file.StandardCopyOption.ATOMIC_MOVE);
        } catch (IOException ignored) {
            Files.move(source, destination, java.nio.file.StandardCopyOption.REPLACE_EXISTING);
        }
    }

    private static boolean isWindows() {
        return System.getProperty("os.name", "").toLowerCase().contains("win");
    }

    private static String quoteForCmd(String value) {
        String escaped = value.replace("%", "%%").replace("^", "^^").replace("&", "^&")
                .replace("|", "^|").replace("<", "^<").replace(">", "^>");
        return "\"" + escaped.replace("\"", "\\\"") + "\"";
    }
}
