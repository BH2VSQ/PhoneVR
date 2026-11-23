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
    // Daydream's controller-local +Z is forward; OpenVR's controller-local -Z is forward.
    // This basis conversion preserves a right-handed rotation while changing the forward axis.
    return NormalizeQuaternion({-input.x, -input.y, input.z, input.w});
}

struct DaydreamPoseOutput {
    DaydreamQuaternion orientation{};
    std::array<double, 3> position{};
    std::array<double, 3> velocity{};
    std::array<double, 3> angularVelocity{};
    bool menuPressed = false;
    bool recentered = false;
};

/**
 * Google Daydream-style 3DoF arm model, expressed in OpenVR's X-right, Y-up, Z-back frame.
 * It estimates hand position from controller orientation and HMD pose; it is not 6DoF tracking.
 */
class DaydreamArmModel {
public:
    static constexpr uint64_t kAppRecenterHoldNs = 800000000ULL;
    static constexpr uint64_t kMenuPulseNs = 140000000ULL;

    DaydreamPoseOutput Update(const DaydreamQuaternion& rawDaydreamOrientation,
                              const std::array<double, 3>& headPosition,
                              const DaydreamQuaternion& headOrientation,
                              const std::array<double, 3>& headAngularVelocity,
                              uint64_t nowNs,
                              bool appButtonPressed,
                              bool isLeftHand) {
        DaydreamPoseOutput output{};
        const DaydreamQuaternion rawOpenVr = DaydreamToOpenVR(rawDaydreamOrientation);
        const DaydreamQuaternion headQ = NormalizeQuaternion(headOrientation);

        if (!m_hasOrientationAlignment) {
            // Establish an intuitive forward reference on first connection. The user can
            // repeat this at any time by holding the App button while aiming forward.
            m_orientationAlignment = MultiplyQuaternion(headQ, InverseQuaternion(rawOpenVr));
            m_hasOrientationAlignment = true;
            m_torsoRotation = HorizontalHeadRotation(headQ);
            m_torsoInitialized = true;
        }

        if (appButtonPressed && !m_appWasPressed) {
            m_appPressedAtNs = nowNs;
            m_appLongPressConsumed = false;
        }
        if (appButtonPressed && !m_appLongPressConsumed && m_appPressedAtNs != 0 &&
            nowNs >= m_appPressedAtNs && nowNs - m_appPressedAtNs >= kAppRecenterHoldNs) {
            m_orientationAlignment = MultiplyQuaternion(headQ, InverseQuaternion(rawOpenVr));
            m_appLongPressConsumed = true;
            m_torsoRotation = HorizontalHeadRotation(headQ);
            m_torsoInitialized = true;
            output.recentered = true;
        }
        if (!appButtonPressed && m_appWasPressed) {
            if (!m_appLongPressConsumed) m_menuPulseUntilNs = nowNs + kMenuPulseNs;
            m_appPressedAtNs = 0;
            m_appLongPressConsumed = false;
        }
        m_appWasPressed = appButtonPressed;
        output.menuPressed = nowNs < m_menuPulseUntilNs;

        const DaydreamQuaternion controllerWorldQ =
            MultiplyQuaternion(m_orientationAlignment, rawOpenVr);
        output.orientation = controllerWorldQ;

        const DaydreamQuaternion targetTorsoQ = HorizontalHeadRotation(headQ);
        if (!m_torsoInitialized) {
            m_torsoRotation = targetTorsoQ;
            m_torsoInitialized = true;
        } else {
            const double angularSpeedRad = std::sqrt(
                headAngularVelocity[0] * headAngularVelocity[0] +
                headAngularVelocity[1] * headAngularVelocity[1] +
                headAngularVelocity[2] * headAngularVelocity[2]);
            const double angularSpeedDeg = angularSpeedRad * 180.0 / kPi;
            const double gazeFilterStrength = std::clamp((angularSpeedDeg - 0.2) / 45.0, 0.0, 0.1);
            m_torsoRotation = Slerp(m_torsoRotation, targetTorsoQ, gazeFilterStrength);
        }

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

        if (!output.recentered && previousPoseNs != 0 && nowNs > previousPoseNs) {
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
    bool m_hasOrientationAlignment = false;
    bool m_torsoInitialized = false;
    bool m_appWasPressed = false;
    bool m_appLongPressConsumed = false;
    bool m_positionInitialized = false;
    uint64_t m_appPressedAtNs = 0;
    uint64_t m_menuPulseUntilNs = 0;
    uint64_t m_lastUpdateNs = 0;
    uint64_t m_lastPoseNs = 0;
    DaydreamQuaternion m_previousOrientation{};
    std::array<double, 3> m_smoothedPosition{};
    std::array<double, 3> m_smoothedVelocity{};
    std::array<double, 3> m_smoothedAngularVelocity{};
};

} // namespace phonevr
