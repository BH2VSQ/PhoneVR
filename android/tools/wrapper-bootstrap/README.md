# PhoneVRStream Gradle bootstrap launcher

This repository-owned Java launcher uses the entry-point class name `org.gradle.wrapper.GradleWrapperMain` so the `gradlew` scripts can invoke it without a system-wide Gradle installation.

The launcher intentionally implements only the features needed by this project:

1. Read `gradle/wrapper/gradle-wrapper.properties`.
2. Download the pinned Gradle distribution from the configured URL.
3. Verify `distributionSha256Sum` before extraction.
4. Extract the distribution under `GRADLE_USER_HOME/wrapper/dists/phonvrstream/`.
5. Forward all command-line arguments to the downloaded Gradle executable.

The small `gradle-wrapper.jar` is generated on demand from the checked-in Java source. The generated JAR is ignored by Git, so a clean checkout and source archive remain reproducible. Both `gradlew` and `gradlew.bat` perform this bootstrap automatically when the JAR is absent. A JDK with `javac` and `jar` is required for the first invocation; Gradle itself does not need to be installed globally.

To generate the launcher manually from the Android project directory:

```sh
sh tools/wrapper-bootstrap/build-wrapper.sh
```

The distribution URL and SHA-256 remain pinned in `gradle/wrapper/gradle-wrapper.properties`. The launcher is project-owned bootstrap code, not the upstream Gradle Wrapper implementation.
