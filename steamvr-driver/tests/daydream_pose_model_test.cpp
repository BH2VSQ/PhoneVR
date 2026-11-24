#include "../src/daydream_pose_model.h"

#include <cstdlib>
#include <cmath>
#include <iostream>

using namespace phonevr;

namespace {
constexpr double kPi = 3.14159265358979323846;

DaydreamQuaternion Yaw(double radians) {
    return {0.0, std::sin(radians * 0.5), 0.0, std::cos(radians * 0.5)};
}

bool Near(double actual, double expected, double epsilon = 1.0e-4) {
    return std::abs(actual - expected) <= epsilon;
}

void Check(bool condition, const char* expression, int line) {
    if (!condition) {
        std::cerr << "Daydream pose model test failed at line " << line << ": " << expression << "\n";
        std::exit(EXIT_FAILURE);
    }
}
}

#define CHECK(condition) Check((condition), #condition, __LINE__)

int main() {
    const std::array<double, 3> headPosition{0.0, 1.65, 0.0};
    const std::array<double, 3> noControllerGyroscope{0.0, 0.0, 0.0};

    // Daydream and OpenVR use different local forward-axis conventions.
    const auto convertedYaw = DaydreamToOpenVR(Yaw(kPi / 2.0));
    const auto convertedForward = RotateVector(convertedYaw, {0.0, 0.0, -1.0});
    CHECK(Near(convertedForward.x, 1.0));
    CHECK(Near(convertedForward.y, 0.0));
    CHECK(Near(convertedForward.z, 0.0));

    // The arm model places the left/right estimates symmetrically around the head.
    DaydreamArmModel leftModel;
    DaydreamArmModel rightModel;
    const auto left = leftModel.Update({}, headPosition, {}, noControllerGyroscope, 1000000000ULL, true);
    const auto right = rightModel.Update({}, headPosition, {}, noControllerGyroscope, 1000000000ULL, false);
    CHECK(left.position[0] < 0.0);
    CHECK(right.position[0] > 0.0);
    CHECK(Near(left.position[0], -right.position[0]));
    CHECK(Near(right.position[0], 0.195));
    CHECK(Near(right.position[1], 1.15));
    CHECK(Near(right.position[2], -0.305));
    CHECK(std::isfinite(left.position[1]) && std::isfinite(left.position[2]));

    // Raising the controller rotates the estimated arm upward.
    DaydreamArmModel raisingModel;
    const auto neutralPose = raisingModel.Update({}, headPosition, {}, noControllerGyroscope,
                                                  2000000000ULL, false);
    const double halfPitch = kPi / 8.0;
    const DaydreamQuaternion pitchUp{-std::sin(halfPitch), 0.0, 0.0, std::cos(halfPitch)};
    const auto raisedPose = raisingModel.Update(pitchUp, headPosition, {}, noControllerGyroscope,
                                                 2010000000ULL, false);
    CHECK(raisedPose.position[1] > neutralPose.position[1]);
    CHECK(std::abs(raisedPose.velocity[1]) > 0.01);

    // GvrArmModel uses the controller gyro, not HMD angular velocity, to adapt torso-following.
    DaydreamArmModel lowGyroModel;
    DaydreamArmModel highGyroModel;
    const auto lowGyroStart = lowGyroModel.Update({}, headPosition, {}, noControllerGyroscope,
                                                   2500000000ULL, false);
    const auto highGyroStart = highGyroModel.Update({}, headPosition, {}, noControllerGyroscope,
                                                     2500000000ULL, false);
    (void)lowGyroStart;
    (void)highGyroStart;
    const DaydreamQuaternion headYaw = Yaw(kPi / 2.0);
    const std::array<double, 3> lowGyro{0.0, 0.0, 0.0};
    const std::array<double, 3> highGyro{0.0, 0.0, 45.0};
    const auto lowGyroPose = lowGyroModel.Update({}, headPosition, headYaw, lowGyro,
                                                  2580000000ULL, false);
    const auto highGyroPose = highGyroModel.Update({}, headPosition, headYaw, highGyro,
                                                    2580000000ULL, false);
    const double gyroDrivenPositionDelta = std::sqrt(
        std::pow(lowGyroPose.position[0] - highGyroPose.position[0], 2.0) +
        std::pow(lowGyroPose.position[1] - highGyroPose.position[1], 2.0) +
        std::pow(lowGyroPose.position[2] - highGyroPose.position[2], 2.0));
    CHECK(gyroDrivenPositionDelta > 0.009);

    // An explicit software recenter aligns the controller's current aim with HMD forward.
    DaydreamArmModel recenterModel;
    const DaydreamQuaternion rotatedController = Yaw(kPi / 4.0);
    (void)recenterModel.Update({}, headPosition, {}, noControllerGyroscope, 3000000000ULL, false);
    auto pose = recenterModel.Update(rotatedController, headPosition, {}, noControllerGyroscope,
                                3010000000ULL, false);
    CHECK(std::abs(pose.angularVelocity[1]) > 0.01);
    recenterModel.Recenter(rotatedController, {});
    pose = recenterModel.Update(rotatedController, headPosition, {}, noControllerGyroscope,
                                3020000000ULL, false);
    CHECK(Near(pose.orientation.x, 0.0));
    CHECK(Near(pose.orientation.y, 0.0));
    CHECK(Near(pose.orientation.z, 0.0));
    CHECK(Near(pose.orientation.w, 1.0));
    CHECK(Near(pose.velocity[0], 0.0));
    CHECK(Near(pose.velocity[1], 0.0));
    CHECK(Near(pose.velocity[2], 0.0));

    // App short press remains a menu action.
    DaydreamButtonGesture shortGesture;
    auto gesture = shortGesture.Update(true, true, 4000000000ULL);
    CHECK(gesture.pressedEdge);
    CHECK(!gesture.longPressTriggered);
    gesture = shortGesture.Update(true, false, 4200000000ULL);
    CHECK(gesture.releasedEdge);
    CHECK(gesture.shortPressTriggered);
    CHECK(!gesture.suppressMenuPulse);

    // An 800 ms App hold triggers exactly one software-recenter request and suppresses the menu.
    DaydreamButtonGesture longGesture;
    gesture = longGesture.Update(true, true, 5000000000ULL);
    CHECK(gesture.pressedEdge);
    gesture = longGesture.Update(true, true, 5799999999ULL);
    CHECK(!gesture.longPressTriggered);
    gesture = longGesture.Update(true, true, 5800000000ULL);
    CHECK(gesture.longPressTriggered);
    CHECK(gesture.suppressMenuPulse);
    gesture = longGesture.Update(true, true, 5900000000ULL);
    CHECK(!gesture.longPressTriggered);
    gesture = longGesture.Update(true, false, 5910000000ULL);
    CHECK(gesture.releasedEdge);
    CHECK(gesture.suppressMenuPulse);
    CHECK(!gesture.shortPressTriggered);

    // Gesture timing continues while HMD tracking is unavailable, as long as BLE input is fresh.
    DaydreamButtonGesture noHmdGesture;
    gesture = noHmdGesture.Update(true, true, 6000000000ULL);
    CHECK(gesture.pressedEdge);
    gesture = noHmdGesture.Update(true, true, 6800000000ULL);
    CHECK(gesture.longPressTriggered);

    // Home short press remains a system-button click; Home long press is also a recenter fallback.
    DaydreamButtonGesture homeGesture;
    gesture = homeGesture.Update(true, true, 7000000000ULL);
    CHECK(gesture.pressedEdge);
    gesture = homeGesture.Update(true, false, 7100000000ULL);
    CHECK(gesture.shortPressTriggered);
    gesture = homeGesture.Update(true, true, 7200000000ULL);
    CHECK(gesture.pressedEdge);
    gesture = homeGesture.Update(true, true, 8000000000ULL);
    CHECK(gesture.longPressTriggered);
    gesture = homeGesture.Update(true, false, 8010000000ULL);
    CHECK(gesture.suppressMenuPulse);
    CHECK(!gesture.shortPressTriggered);

    std::cout << "Daydream pose model tests passed\n";
    return 0;
}
