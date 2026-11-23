from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]
MAIN_ACTIVITY = ROOT / "android/app/src/main/java/com/vsq/phonvrstream/MainActivity.kt"


class AndroidControllerMenuLayoutTests(unittest.TestCase):
    def test_entire_controller_panel_is_inside_vertical_scroll_view(self):
        source = MAIN_ACTIVITY.read_text(encoding="utf-8")
        start = source.index("    private fun buildControllerPanel(): LinearLayout {")
        end = source.index("\n    private fun controllerStatusBackground()", start)
        panel = source[start:end]

        self.assertIn("val contentScroll = ScrollView(this)", panel)
        self.assertIn("isFillViewport = true", panel)
        self.assertIn("isSmoothScrollingEnabled = true", panel)
        self.assertIn("addView(content, FrameLayout.LayoutParams(-1, -2))", panel)
        self.assertIn("panel.addView(contentScroll, LinearLayout.LayoutParams(-1, 0, 1f))", panel)
        self.assertIn("content.addView(controllerCandidateList", panel)
        self.assertIn('makeButton("关闭")', panel)
        self.assertNotIn("val scroll = ScrollView(this)", panel)

    def test_first_level_menu_is_fully_scrollable_with_a_bounded_viewport(self):
        source = MAIN_ACTIVITY.read_text(encoding="utf-8")
        start = source.index("    private fun buildMenuPanel(): LinearLayout {")
        end = source.index("\n    private fun buildControllerPanel(): LinearLayout {", start)
        panel = source[start:end]

        self.assertIn("val menuContent = LinearLayout(this)", panel)
        self.assertIn("val menuScroll = object : ScrollView(this)", panel)
        self.assertIn("override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int)", panel)
        self.assertIn("View.MeasureSpec.AT_MOST", panel)
        self.assertIn("isSmoothScrollingEnabled = true", panel)
        self.assertIn("isVerticalScrollBarEnabled = true", panel)
        self.assertIn("menuScroll.addView(", panel)
        self.assertIn(
            "FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)",
            panel,
        )
        self.assertNotIn("ScrollView.LayoutParams", panel)
        self.assertIn("panel.addView(menuScroll, LinearLayout.LayoutParams(-1, -2))", panel)
        self.assertIn('menuContent.addView(makeButton("Daydream 手柄配对")', panel)
        self.assertNotIn('panel.addView(makeButton("Daydream 手柄配对")', panel)

    def test_screen_rotation_and_optical_preset_buttons_use_same_gap(self):
        source = MAIN_ACTIVITY.read_text(encoding="utf-8")
        self.assertIn(
            "menuContent.addView(screenFlipButton, LinearLayout.LayoutParams(-1, dp(40)).apply { bottomMargin = dp(6) })",
            source,
        )
        self.assertIn(
            'makeButton("光学预设") {',
            source,
        )
        self.assertIn(
            '}.apply { layoutParams = LinearLayout.LayoutParams(-1, dp(40)).apply { bottomMargin = dp(6) } })',
            source,
        )


if __name__ == "__main__":
    unittest.main()
