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

DaydreamQuaternion Pitch(double radians) {
    return {std::sin(radians * 0.5), 0.0, 0.0, std::cos(radians * 0.5)};
}

DaydreamQuaternion Roll(double radians) {
    return {0.0, 0.0, std::sin(radians * 0.5), std::cos(radians * 0.5)};
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

    // Google Daydream Start/Controller Space and OpenVR share the same basis:
    // +X right, +Y up, +Z backward. The physical controller points along local -Z.
    const auto convertedYaw = DaydreamToOpenVR(Yaw(kPi / 2.0));
    const auto yawForward = RotateVector(convertedYaw, {0.0, 0.0, -1.0});
    CHECK(Near(yawForward.x, -1.0)); // Positive Y yaw turns the forward vector to the left.
    CHECK(Near(yawForward.y, 0.0));
    CHECK(Near(yawForward.z, 0.0));

    const DaydreamQuaternion qPitchUp{std::sin(kPi / 4.0), 0.0, 0.0, std::cos(kPi / 4.0)};
    const auto pitchForward = RotateVector(DaydreamToOpenVR(qPitchUp), {0.0, 0.0, -1.0});
    CHECK(Near(pitchForward.x, 0.0));
    CHECK(Near(pitchForward.y, 1.0));
    CHECK(Near(pitchForward.z, 0.0));

    const DaydreamQuaternion rollLeft{0.0, 0.0, std::sin(kPi / 4.0), std::cos(kPi / 4.0)};
    const auto rolledUp = RotateVector(DaydreamToOpenVR(rollLeft), {0.0, 1.0, 0.0});
    CHECK(Near(rolledUp.x, -1.0));
    CHECK(Near(rolledUp.y, 0.0));
    CHECK(Near(rolledUp.z, 0.0));

    // Daydream BLE touch coordinates are normalized with a top-left origin and +Y down.
    // SteamVR's trackpad +X/+Y must therefore follow right/up finger movement.
    const auto untouchedPad = DaydreamTouchpadToOpenVR(0.9, 0.1, false);
    CHECK(Near(untouchedPad[0], 0.0) && Near(untouchedPad[1], 0.0));
    const auto padCenter = DaydreamTouchpadToOpenVR(0.5, 0.5, true);
    CHECK(Near(padCenter[0], 0.0) && Near(padCenter[1], 0.0));
    const auto padUpperRight = DaydreamTouchpadToOpenVR(1.0, 0.0, true);
    CHECK(Near(padUpperRight[0], std::sqrt(0.5)) && Near(padUpperRight[1], std::sqrt(0.5)));
    const auto padLowerLeft = DaydreamTouchpadToOpenVR(0.0, 1.0, true);
    CHECK(Near(padLowerLeft[0], -std::sqrt(0.5)) && Near(padLowerLeft[1], -std::sqrt(0.5)));
    const auto padTop = DaydreamTouchpadToOpenVR(0.5, 0.0, true);
    const auto padBottom = DaydreamTouchpadToOpenVR(0.5, 1.0, true);
    CHECK(padTop[1] > 0.99 && padBottom[1] < -0.99);
    const auto padRight = DaydreamTouchpadToOpenVR(1.0, 0.5, true);
    const auto padLeft = DaydreamTouchpadToOpenVR(0.0, 0.5, true);
    CHECK(padRight[0] > 0.99 && padLeft[0] < -0.99);
    const auto padDeadzone = DaydreamTouchpadToOpenVR(0.55, 0.5, true);
    CHECK(Near(padDeadzone[0], 0.0) && Near(padDeadzone[1], 0.0));
    CHECK(std::hypot(padUpperRight[0], padUpperRight[1]) <= 1.0 + 1.0e-6);

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
    const DaydreamQuaternion pitchUp{std::sin(halfPitch), 0.0, 0.0, std::cos(halfPitch)};
    const auto raisedPose = raisingModel.Update(pitchUp, headPosition, {}, noControllerGyroscope,
                                                 2010000000ULL, false);
    CHECK(raisedPose.position[1] > neutralPose.position[1]);
    CHECK(std::abs(raisedPose.velocity[1]) > 0.01);

    // HMD yaw must move the controller reference even if the controller gyro is still.
    DaydreamArmModel headRelativeModel;
    const DaydreamQuaternion headYaw = Yaw(kPi / 2.0);
    const auto headRelativeStart = headRelativeModel.Update(
        {}, headPosition, {}, noControllerGyroscope, 2500000000ULL, false);
    const auto headRelativeTurn = headRelativeModel.Update(
        {}, headPosition, headYaw, noControllerGyroscope, 2580000000ULL, false);
    (void)headRelativeStart;
    const auto headRelativeForward = RotateVector(
        headRelativeTurn.orientation, {0.0, 0.0, -1.0});
    CHECK(headRelativeForward.x < -0.99);
    CHECK(std::abs(headRelativeForward.z) < 0.02);

    // Initial tracking preserves Google's Start Space orientation without an implicit HMD snap.
    DaydreamArmModel directOrientationModel;
    const auto directOrientationPose = directOrientationModel.Update(
        qPitchUp, headPosition, {}, noControllerGyroscope, 2900000000ULL, false);
    CHECK(Near(directOrientationPose.orientation.x, qPitchUp.x));
    CHECK(Near(directOrientationPose.orientation.y, qPitchUp.y));
    CHECK(Near(directOrientationPose.orientation.z, qPitchUp.z));
    CHECK(Near(directOrientationPose.orientation.w, qPitchUp.w));

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

    // Recenter corrects horizontal heading only. Controller pitch and roll are
    // preserved, and HMD pitch/roll do not leak into the controller's aim axis.
    DaydreamArmModel yawOnlyRecenterModel;
    const DaydreamQuaternion controllerTilt = MultiplyQuaternion(
        MultiplyQuaternion(Yaw(kPi / 4.0), Pitch(kPi / 6.0)), Roll(kPi / 9.0));
    const DaydreamQuaternion tiltedHead = MultiplyQuaternion(
        MultiplyQuaternion(Yaw(-kPi / 6.0), Pitch(-kPi / 12.0)), Roll(kPi / 18.0));
    yawOnlyRecenterModel.Recenter(controllerTilt, tiltedHead);
    const auto yawOnlyPose = yawOnlyRecenterModel.Update(
        controllerTilt, headPosition, tiltedHead, noControllerGyroscope,
        3030000000ULL, false);
    const DaydreamQuaternion expectedYawOnly = MultiplyQuaternion(
        MultiplyQuaternion(Yaw(-kPi / 6.0), Pitch(kPi / 6.0)), Roll(kPi / 9.0));
    CHECK(Near(yawOnlyPose.orientation.x, expectedYawOnly.x));
    CHECK(Near(yawOnlyPose.orientation.y, expectedYawOnly.y));
    CHECK(Near(yawOnlyPose.orientation.z, expectedYawOnly.z));
    CHECK(Near(yawOnlyPose.orientation.w, expectedYawOnly.w));

    // App is now exposed as a normal held boolean menu input. Home alone keeps
    // the 800 ms long-press gesture for software recenter.
    // Home short press remains a system-button click; Home long press is also a recenter fallback.
    DaydreamButtonGesture homeGesture;
    auto gesture = homeGesture.Update(true, true, 7000000000ULL);
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
