// Derived from the Google VR SDK GvrArmModel algorithm and default tuning values.
// Copyright 2016 Google Inc.
// Licensed under the Apache License, Version 2.0. See the project license at:
// https://www.apache.org/licenses/LICENSE-2.0
#pragma once

#include <algorithm>
#include <array>
#include <cmath>
#include <cstdint>

namespace phonevr {

struct DaydreamQuaternion {
    double x = 0.0;
    double y = 0.0;
    double z = 0.0;
    double w = 1.0;
};

struct DaydreamVector3 {
    double x = 0.0;
    double y = 0.0;
    double z = 0.0;
};

inline DaydreamQuaternion NormalizeQuaternion(const DaydreamQuaternion& q) {
    const double norm = std::sqrt(q.x * q.x + q.y * q.y + q.z * q.z + q.w * q.w);
    if (!std::isfinite(norm) || norm < 1.0e-9) return {};
    return {q.x / norm, q.y / norm, q.z / norm, q.w / norm};
}

inline DaydreamQuaternion MultiplyQuaternion(const DaydreamQuaternion& a, const DaydreamQuaternion& b) {
    return NormalizeQuaternion({
        a.w * b.x + a.x * b.w + a.y * b.z - a.z * b.y,
        a.w * b.y - a.x * b.z + a.y * b.w + a.z * b.x,
        a.w * b.z + a.x * b.y - a.y * b.x + a.z * b.w,
        a.w * b.w - a.x * b.x - a.y * b.y - a.z * b.z,
    });
}

inline DaydreamQuaternion InverseQuaternion(const DaydreamQuaternion& q) {
    const auto normalized = NormalizeQuaternion(q);
    return {-normalized.x, -normalized.y, -normalized.z, normalized.w};
}

inline DaydreamVector3 RotateVector(const DaydreamQuaternion& q, const DaydreamVector3& v) {
    const DaydreamQuaternion p{v.x, v.y, v.z, 0.0};
    const DaydreamQuaternion normalized = NormalizeQuaternion(q);
    const DaydreamQuaternion inverse{-normalized.x, -normalized.y, -normalized.z, normalized.w};
    const DaydreamQuaternion rotatedRaw{
        normalized.w * p.x + normalized.x * p.w + normalized.y * p.z - normalized.z * p.y,
        normalized.w * p.y - normalized.x * p.z + normalized.y * p.w + normalized.z * p.x,
        normalized.w * p.z + normalized.x * p.y - normalized.y * p.x + normalized.z * p.w,
        normalized.w * p.w - normalized.x * p.x - normalized.y * p.y - normalized.z * p.z,
    };
    const DaydreamQuaternion rotated{
        rotatedRaw.w * inverse.x + rotatedRaw.x * inverse.w + rotatedRaw.y * inverse.z - rotatedRaw.z * inverse.y,
        rotatedRaw.w * inverse.y - rotatedRaw.x * inverse.z + rotatedRaw.y * inverse.w + rotatedRaw.z * inverse.x,
        rotatedRaw.w * inverse.z + rotatedRaw.x * inverse.y - rotatedRaw.y * inverse.x + rotatedRaw.z * inverse.w,
        0.0,
    };
    return {rotated.x, rotated.y, rotated.z};
}

inline double Dot(const DaydreamVector3& a, const DaydreamVector3& b) {
    return a.x * b.x + a.y * b.y + a.z * b.z;
}

inline DaydreamVector3 NormalizeVector(const DaydreamVector3& value) {
    const double length = std::sqrt(Dot(value, value));
    if (!std::isfinite(length) || length < 1.0e-9) return {0.0, 0.0, -1.0};
    return {value.x / length, value.y / length, value.z / length};
}

inline DaydreamQuaternion RotationBetweenVectors(const DaydreamVector3& fromValue,
                                                  const DaydreamVector3& toValue) {
    const DaydreamVector3 from = NormalizeVector(fromValue);
    const DaydreamVector3 to = NormalizeVector(toValue);
    const double dot = std::clamp(Dot(from, to), -1.0, 1.0);
    if (dot > 0.999999) return {};
    if (dot < -0.999999) {
        DaydreamVector3 axis = std::abs(from.x) < 0.8
            ? DaydreamVector3{0.0, -from.z, from.y}
            : DaydreamVector3{-from.y, from.x, 0.0};
        axis = NormalizeVector(axis);
        return {axis.x, axis.y, axis.z, 0.0};
    }
    // Cross(from, to), followed by normalization, is the shortest rotation.
    const DaydreamVector3 cross{
        from.y * to.z - from.z * to.y,
        from.z * to.x - from.x * to.z,
        from.x * to.y - from.y * to.x,
    };
    return NormalizeQuaternion({cross.x, cross.y, cross.z, 1.0 + dot});
}

inline DaydreamQuaternion NlerpIdentity(const DaydreamQuaternion& target, double amount) {
    const double t = std::clamp(amount, 0.0, 1.0);
    DaydreamQuaternion value{
        target.x * t,
        target.y * t,
        target.z * t,
        (1.0 - t) + target.w * t,
    };
    if (value.w < 0.0) value = {-value.x, -value.y, -value.z, -value.w};
    return NormalizeQuaternion(value);
}

inline DaydreamQuaternion DaydreamToOpenVR(const DaydreamQuaternion& input) {
    // Google Daydream Controller Space and OpenVR use the same right-handed basis:
    // +X right, +Y up, +Z backward. Therefore the physical controller's forward
    // direction is -Z in both APIs and the quaternion must not be reflected.
    return NormalizeQuaternion(input);
}

// Daydream touch coordinates are normalized with the controller protocol's
// origin at the bottom-left: X increases right and Y increases up. The driver
// maps them to SteamVR axes (+X right, +Y up). Untouched state returns to zero.
inline std::array<double, 2> DaydreamTouchpadToOpenVR(double touchX,
                                                       double touchY,
                                                       bool touching) {
    // The BLE protocol reports X left-to-right and Y bottom-to-top.
    // Keep both axes in the same direction as SteamVR's +X right / +Y up.
    if (!touching) return {0.0, 0.0};

    double x = std::clamp(touchX * 2.0 - 1.0, -1.0, 1.0);
    double y = std::clamp(touchY * 2.0 - 1.0, -1.0, 1.0);
    const double magnitude = std::sqrt(x * x + y * y);
    constexpr double kDeadzone = 0.15;
    if (!std::isfinite(magnitude) || magnitude <= kDeadzone) return {0.0, 0.0};

    // A circular gate prevents diagonal movement from being faster than
    // cardinal movement. Rescale the remaining range and soften the centre
    // response for a touchpad-to-analogue-stick feel.
    const double normalizedMagnitude = std::min(magnitude, 1.0);
    const double scaled = std::clamp(
        (normalizedMagnitude - kDeadzone) / (1.0 - kDeadzone), 0.0, 1.0);
    const double response = 0.35 * scaled + 0.65 * scaled * scaled * scaled;
    const double inverseMagnitude = 1.0 / magnitude;
    return {x * inverseMagnitude * response, y * inverseMagnitude * response};
}

struct DaydreamButtonGestureResult {
    bool pressedEdge = false;
    bool releasedEdge = false;
    bool longPressTriggered = false;
    bool shortPressTriggered = false;
    bool suppressMenuPulse = false;
};

/** Detects controller button gestures from fresh BLE input, independent of HMD tracking validity. */
class DaydreamButtonGesture {
public:
    static constexpr uint64_t kLongPressNs = 800000000ULL;

    DaydreamButtonGestureResult Update(bool inputFresh, bool pressed, uint64_t nowNs) {
        DaydreamButtonGestureResult result{};
        if (!inputFresh) {
            if (m_wasPressed) {
                result.releasedEdge = true;
                result.suppressMenuPulse = m_longPressConsumed;
            }
            Reset();
            return result;
        }

        if (pressed && !m_wasPressed) {
            m_pressedAtNs = nowNs;
            m_longPressConsumed = false;
            result.pressedEdge = true;
        }
        if (pressed && !m_longPressConsumed && m_pressedAtNs != 0 &&
            nowNs >= m_pressedAtNs && nowNs - m_pressedAtNs >= kLongPressNs) {
            m_longPressConsumed = true;
            result.longPressTriggered = true;
        }
        if (!pressed && m_wasPressed) {
            result.releasedEdge = true;
            result.suppressMenuPulse = m_longPressConsumed;
            result.shortPressTriggered = !m_longPressConsumed;
            m_pressedAtNs = 0;
            m_longPressConsumed = false;
        } else {
            result.suppressMenuPulse = pressed && m_longPressConsumed;
        }
        m_wasPressed = pressed;
        return result;
    }

private:
    void Reset() {
        m_wasPressed = false;
        m_longPressConsumed = false;
        m_pressedAtNs = 0;
    }

    bool m_wasPressed = false;
    bool m_longPressConsumed = false;
    uint64_t m_pressedAtNs = 0;
};

struct DaydreamPoseOutput {
    DaydreamQuaternion orientation{};
    std::array<double, 3> position{};
    std::array<double, 3> velocity{};
    std::array<double, 3> angularVelocity{};
};

/**
 * Google Daydream-style 3DoF arm model, expressed in OpenVR's X-right, Y-up, Z-back frame.
 * It estimates hand position from controller orientation and HMD pose; it is not 6DoF tracking.
 */
class DaydreamArmModel {
public:
    void Recenter(const DaydreamQuaternion& rawDaydreamOrientation,
                  const DaydreamQuaternion& headOrientation) {
        const DaydreamQuaternion rawOpenVr = DaydreamToOpenVR(rawDaydreamOrientation);
        const DaydreamQuaternion headQ = NormalizeQuaternion(headOrientation);
        // Recenter yaw only. A full head/controller quaternion match also cancels
        // controller pitch and roll, which makes pointing directions appear to flip
        // whenever the user tilts their head or holds the remote at an angle.
        const DaydreamQuaternion controllerHeading = HorizontalHeadRotation(rawOpenVr);
        const DaydreamQuaternion headHeading = HorizontalHeadRotation(headQ);
        m_orientationAlignment = MultiplyQuaternion(
            headHeading, InverseQuaternion(controllerHeading));
        m_hasOrientationAlignment = true;
        m_torsoRotation = headHeading;
        m_torsoInitialized = true;
        m_lastHeadUpdateNs = 0;
        m_positionInitialized = false;
        m_lastUpdateNs = 0;
        m_lastPoseNs = 0;
        m_smoothedVelocity = {};
        m_smoothedAngularVelocity = {};
    }

    DaydreamPoseOutput Update(const DaydreamQuaternion& rawDaydreamOrientation,
                              const std::array<double, 3>& headPosition,
                              const DaydreamQuaternion& headOrientation,
                              const std::array<double, 3>& controllerGyroscope,
                              uint64_t nowNs,
                              bool isLeftHand) {
        DaydreamPoseOutput output{};
        const DaydreamQuaternion rawOpenVr = DaydreamToOpenVR(rawDaydreamOrientation);
        const DaydreamQuaternion headQ = NormalizeQuaternion(headOrientation);
        (void)controllerGyroscope; // HMD yaw, not controller motion, drives the reference frame.

        const DaydreamQuaternion targetTorsoQ = HorizontalHeadRotation(headQ);
        // Controller orientation is already expressed in Daydream Start Space,
        // which is aligned with the HMD when tracking begins. Do not apply the
        // current HMD yaw delta here: doing so rotates a stationary controller
        // whenever the user turns their head and creates visible pointing drift.
        // The torso frame below follows HMD yaw separately for arm positioning.
        const DaydreamQuaternion controllerWorldQ = MultiplyQuaternion(
            m_orientationAlignment, rawOpenVr);
        output.orientation = controllerWorldQ;

        if (!m_torsoInitialized) {
            m_torsoRotation = targetTorsoQ;
            m_torsoInitialized = true;
        } else if (m_lastHeadUpdateNs == 0 || nowNs <= m_lastHeadUpdateNs) {
            m_torsoRotation = targetTorsoQ;
        } else {
            // Follow HMD yaw continuously with a time-based filter. This remains
            // responsive when the controller gyro is stationary or unavailable.
            const double dt = std::clamp(
                static_cast<double>(nowNs - m_lastHeadUpdateNs) / 1.0e9, 0.0, 0.1);
            constexpr double kHeadFollowTimeConstantSeconds = 0.020;
            const double follow = 1.0 - std::exp(-dt / kHeadFollowTimeConstantSeconds);
            m_torsoRotation = Slerp(m_torsoRotation, targetTorsoQ, follow);
        }
        m_lastHeadUpdateNs = nowNs;

        const DaydreamQuaternion relativeControllerQ =
            MultiplyQuaternion(InverseQuaternion(m_torsoRotation), controllerWorldQ);
        const DaydreamVector3 controllerForward = NormalizeVector(
            RotateVector(relativeControllerQ, {0.0, 0.0, -1.0}));
        const double xAngleDegrees = std::asin(std::clamp(controllerForward.y, -1.0, 1.0)) *
            180.0 / kPi;
        const double extensionRatio = std::clamp((xAngleDegrees - 7.0) / (60.0 - 7.0), 0.0, 1.0);
        const DaydreamQuaternion xyRotation =
            RotationBetweenVectors({0.0, 0.0, -1.0}, controllerForward);

        const double totalAngleDegrees = 2.0 * std::acos(
            std::clamp(std::abs(xyRotation.w), 0.0, 1.0)) * 180.0 / kPi;
        const double lerpSuppression = 1.0 - std::pow(totalAngleDegrees / 180.0, 6.0);
        constexpr double elbowBendRatio = 0.6;
        constexpr double extensionWeight = 0.4;
        const double lerpValue = ((1.0 - elbowBendRatio) +
            elbowBendRatio * extensionRatio * extensionWeight) * lerpSuppression;
        const DaydreamQuaternion lerpRotation = NlerpIdentity(xyRotation, lerpValue);

        const double handed = isLeftHand ? -1.0 : 1.0;
        DaydreamVector3 elbowPosition{0.195 * handed, -0.5, -0.005};
        const DaydreamVector3 extensionOffset{-0.13 * handed, 0.14, -0.08};
        elbowPosition.x += extensionOffset.x * extensionRatio;
        elbowPosition.y += extensionOffset.y * extensionRatio;
        elbowPosition.z += extensionOffset.z * extensionRatio;

        const DaydreamQuaternion elbowRotation = MultiplyQuaternion(
            MultiplyQuaternion(m_torsoRotation, InverseQuaternion(lerpRotation)), xyRotation);
        const DaydreamQuaternion wristRotation = MultiplyQuaternion(elbowRotation, lerpRotation);
        const DaydreamVector3 elbowWorldOffset = RotateVector(m_torsoRotation, elbowPosition);
        const DaydreamVector3 wristWorldOffset = RotateVector(
            elbowRotation, {0.0, 0.0, -0.25});
        const DaydreamVector3 controllerWorldOffset = RotateVector(
            wristRotation, {0.0, 0.0, -0.05});
        const std::array<double, 3> targetPosition{
            headPosition[0] + elbowWorldOffset.x + wristWorldOffset.x + controllerWorldOffset.x,
            headPosition[1] + elbowWorldOffset.y + wristWorldOffset.y + controllerWorldOffset.y,
            headPosition[2] + elbowWorldOffset.z + wristWorldOffset.z + controllerWorldOffset.z,
        };

        const auto previousPosition = m_smoothedPosition;
        const DaydreamQuaternion previousOrientation = m_previousOrientation;
        const uint64_t previousPoseNs = m_lastPoseNs;
        if (!m_positionInitialized || m_lastUpdateNs == 0 || nowNs <= m_lastUpdateNs) {
            m_smoothedPosition = targetPosition;
            m_positionInitialized = true;
        } else {
            const double dt = std::clamp(static_cast<double>(nowNs - m_lastUpdateNs) / 1.0e9, 0.0, 0.1);
            const double alpha = 1.0 - std::exp(-dt / 0.045);
            for (size_t index = 0; index < m_smoothedPosition.size(); ++index) {
                m_smoothedPosition[index] += (targetPosition[index] - m_smoothedPosition[index]) * alpha;
            }
        }
        m_lastUpdateNs = nowNs;
        output.position = m_smoothedPosition;

        if (previousPoseNs != 0 && nowNs > previousPoseNs) {
            const double dt = std::clamp(static_cast<double>(nowNs - previousPoseNs) / 1.0e9, 1.0e-4, 0.1);
            const double velocityAlpha = 1.0 - std::exp(-dt / 0.035);
            for (size_t index = 0; index < output.velocity.size(); ++index) {
                const double rawVelocity = (m_smoothedPosition[index] - previousPosition[index]) / dt;
                const double boundedVelocity = std::clamp(rawVelocity, -6.0, 6.0);
                m_smoothedVelocity[index] += (boundedVelocity - m_smoothedVelocity[index]) * velocityAlpha;
                output.velocity[index] = m_smoothedVelocity[index];
            }

            DaydreamQuaternion delta = MultiplyQuaternion(controllerWorldQ, InverseQuaternion(previousOrientation));
            if (delta.w < 0.0) delta = {-delta.x, -delta.y, -delta.z, -delta.w};
            const double vectorLength = std::sqrt(delta.x * delta.x + delta.y * delta.y + delta.z * delta.z);
            if (vectorLength > 1.0e-8) {
                const double angle = 2.0 * std::atan2(vectorLength, std::clamp(delta.w, -1.0, 1.0));
                const double angularSpeed = std::min(angle / dt, 20.0);
                const double angularAlpha = 1.0 - std::exp(-dt / 0.025);
                const std::array<double, 3> rawAngularVelocity{
                    delta.x / vectorLength * angularSpeed,
                    delta.y / vectorLength * angularSpeed,
                    delta.z / vectorLength * angularSpeed,
                };
                for (size_t index = 0; index < output.angularVelocity.size(); ++index) {
                    m_smoothedAngularVelocity[index] +=
                        (rawAngularVelocity[index] - m_smoothedAngularVelocity[index]) * angularAlpha;
                    output.angularVelocity[index] = m_smoothedAngularVelocity[index];
                }
            } else {
                const double angularAlpha = 1.0 - std::exp(-dt / 0.025);
                for (size_t index = 0; index < output.angularVelocity.size(); ++index) {
                    m_smoothedAngularVelocity[index] *= (1.0 - angularAlpha);
                    output.angularVelocity[index] = m_smoothedAngularVelocity[index];
                }
            }
        } else {
            output.velocity = {};
            output.angularVelocity = {};
            m_smoothedVelocity = {};
            m_smoothedAngularVelocity = {};
        }
        m_previousOrientation = controllerWorldQ;
        m_lastPoseNs = nowNs;
        return output;
    }

private:
    static constexpr double kPi = 3.14159265358979323846;

    static DaydreamQuaternion Slerp(const DaydreamQuaternion& fromValue,
                                    const DaydreamQuaternion& toValue,
                                    double amount) {
        DaydreamQuaternion from = NormalizeQuaternion(fromValue);
        DaydreamQuaternion to = NormalizeQuaternion(toValue);
        double dot = from.x * to.x + from.y * to.y + from.z * to.z + from.w * to.w;
        if (dot < 0.0) {
            to = {-to.x, -to.y, -to.z, -to.w};
            dot = -dot;
        }
        if (dot > 0.9995) {
            return NormalizeQuaternion({
                from.x + (to.x - from.x) * amount,
                from.y + (to.y - from.y) * amount,
                from.z + (to.z - from.z) * amount,
                from.w + (to.w - from.w) * amount,
            });
        }
        const double theta = std::acos(std::clamp(dot, -1.0, 1.0));
        const double sinTheta = std::sin(theta);
        const double a = std::sin((1.0 - amount) * theta) / sinTheta;
        const double b = std::sin(amount * theta) / sinTheta;
        return NormalizeQuaternion({
            from.x * a + to.x * b,
            from.y * a + to.y * b,
            from.z * a + to.z * b,
            from.w * a + to.w * b,
        });
    }

    static DaydreamQuaternion HorizontalHeadRotation(const DaydreamQuaternion& headQ) {
        const DaydreamVector3 forward = RotateVector(headQ, {0.0, 0.0, -1.0});
        return RotationBetweenVectors({0.0, 0.0, -1.0},
                                      NormalizeVector({forward.x, 0.0, forward.z}));
    }

    DaydreamQuaternion m_orientationAlignment{};
    DaydreamQuaternion m_torsoRotation{};
    // Google defines Daydream Start Space to coincide with headset space when
    // tracking starts. Keep the raw quaternion until the user explicitly recenters.
    bool m_hasOrientationAlignment = true;
    bool m_torsoInitialized = false;
    bool m_positionInitialized = false;
    uint64_t m_lastHeadUpdateNs = 0;
    uint64_t m_lastUpdateNs = 0;
    uint64_t m_lastPoseNs = 0;
    DaydreamQuaternion m_previousOrientation{};
    std::array<double, 3> m_smoothedPosition{};
    std::array<double, 3> m_smoothedVelocity{};
    std::array<double, 3> m_smoothedAngularVelocity{};
};

} // namespace phonevr
