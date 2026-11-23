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
    const std::array<double, 3> noAngularVelocity{0.0, 0.0, 0.0};

    // A positive Daydream yaw rotates the controller's local +Z forward axis
    // into the corresponding OpenVR local -Z forward direction.
    const auto convertedYaw = DaydreamToOpenVR(Yaw(kPi / 2.0));
    const auto convertedForward = RotateVector(convertedYaw, {0.0, 0.0, -1.0});
    CHECK(Near(convertedForward.x, 1.0));
    CHECK(Near(convertedForward.y, 0.0));
    CHECK(Near(convertedForward.z, 0.0));

    // The arm model places the left/right estimates symmetrically around the head.
    DaydreamArmModel leftModel;
    DaydreamArmModel rightModel;
    const auto left = leftModel.Update({}, headPosition, {}, noAngularVelocity, 1000000000ULL, false, true);
    const auto right = rightModel.Update({}, headPosition, {}, noAngularVelocity, 1000000000ULL, false, false);
    CHECK(left.position[0] < 0.0);
    CHECK(right.position[0] > 0.0);
    CHECK(Near(left.position[0], -right.position[0]));
    CHECK(std::isfinite(left.position[1]) && std::isfinite(left.position[2]));

    // Raising the controller rotates the estimated arm upward, providing a
    // 3DoF-derived virtual position change without claiming physical translation.
    DaydreamArmModel raisingModel;
    const auto neutralPose = raisingModel.Update({}, headPosition, {}, noAngularVelocity,
                                                  2000000000ULL, false, false);
    const double halfPitch = kPi / 8.0;
    const DaydreamQuaternion pitchUp{-std::sin(halfPitch), 0.0, 0.0, std::cos(halfPitch)};
    const auto raisedPose = raisingModel.Update(pitchUp, headPosition, {}, noAngularVelocity,
                                                 2010000000ULL, false, false);
    CHECK(raisedPose.position[1] > neutralPose.position[1]);
    CHECK(std::abs(raisedPose.velocity[1]) > 0.01);

    // First-sample alignment points forward, and an App long press can re-align later.
    DaydreamArmModel recenterModel;
    auto pose = recenterModel.Update({}, headPosition, {}, noAngularVelocity, 1000000000ULL, false, false);
    CHECK(Near(pose.orientation.x, 0.0));
    CHECK(Near(pose.orientation.y, 0.0));
    CHECK(Near(pose.orientation.z, 0.0));
    CHECK(Near(pose.orientation.w, 1.0));

    const DaydreamQuaternion rotatedController = Yaw(kPi / 4.0);
    pose = recenterModel.Update(rotatedController, headPosition, {}, noAngularVelocity,
                                1010000000ULL, true, false);
    CHECK(!pose.menuPressed && !pose.recentered);
    CHECK(std::abs(pose.angularVelocity[1]) > 0.01);
    CHECK(std::isfinite(pose.velocity[0]));
    pose = recenterModel.Update(rotatedController, headPosition, {}, noAngularVelocity,
                                1810000000ULL, true, false);
    CHECK(pose.recentered);
    CHECK(!pose.menuPressed);
    CHECK(Near(pose.angularVelocity[0], 0.0));
    CHECK(Near(pose.angularVelocity[1], 0.0));
    CHECK(Near(pose.angularVelocity[2], 0.0));
    CHECK(Near(pose.orientation.x, 0.0));
    CHECK(Near(pose.orientation.y, 0.0));
    CHECK(Near(pose.orientation.z, 0.0));
    CHECK(Near(pose.orientation.w, 1.0));

    // A short App press still produces a menu pulse; a long press is consumed by recenter.
    pose = recenterModel.Update(rotatedController, headPosition, {}, noAngularVelocity,
                                1820000000ULL, false, false);
    CHECK(!pose.menuPressed);
    pose = recenterModel.Update(rotatedController, headPosition, {}, noAngularVelocity,
                                1900000000ULL, true, false);
    CHECK(!pose.menuPressed);
    pose = recenterModel.Update(rotatedController, headPosition, {}, noAngularVelocity,
                                1950000000ULL, false, false);
    CHECK(pose.menuPressed);
    pose = recenterModel.Update(rotatedController, headPosition, {}, noAngularVelocity,
                                2100000000ULL, false, false);
    CHECK(!pose.menuPressed);

    std::cout << "Daydream pose model tests passed\n";
    return 0;
}
