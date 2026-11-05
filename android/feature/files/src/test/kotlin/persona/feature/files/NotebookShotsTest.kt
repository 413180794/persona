package persona.feature.files

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * The notebook viewer drawn on the JVM with Robolectric's native graphics, in every style, light and dark: a Markdown
 * cell, a code cell with streams, a table's text and a result, an error. Written to `build/files-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class NotebookShotsTest {
    @get:Rule val rule = createComposeRule()

    private val notebook = """{"nbformat":4,"nbformat_minor":5,
      "metadata":{"kernelspec":{"display_name":"Python 3","language":"python"}},
      "cells":[
        {"cell_type":"markdown","source":"## 销量分析\n先读数据，再按地区汇总。"},
        {"cell_type":"code","execution_count":1,"source":"import pandas as pd\n# 读数据\ndf = pd.read_csv(\"sales.csv\")\nprint(len(df))\ndf.head(2)","outputs":[
          {"output_type":"stream","name":"stdout","text":"44\n"},
          {"output_type":"stream","name":"stderr","text":"DeprecationWarning: old api\n"},
          {"output_type":"execute_result","execution_count":1,"data":{"text/html":"<table><tr><th></th><th>地区</th><th>数量</th></tr><tr><td>0</td><td>华东</td><td>1200</td></tr><tr><td>1</td><td>华南</td><td>-80</td></tr></table>","text/plain":"df"}}]},
        {"cell_type":"code","execution_count":2,"source":"1 / 0","outputs":[
          {"output_type":"error","ename":"ZeroDivisionError","evalue":"division by zero","traceback":["\u001b[31m---------------------------------------------------------------------------\u001b[0m","ZeroDivisionError  Traceback (most recent call last)","Cell In[2], line 1\n----> 1 1 / 0"]}]},
        {"cell_type":"code","execution_count":null,"source":"","outputs":[]}
      ]}"""

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/files-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        // The notebook parses off the main thread, which waitForIdle doesn't wait for.
        rule.waitUntil(10_000) { rule.onAllNodesWithText("ZeroDivisionError: division by zero", substring = true).fetchSemanticsNodes().isNotEmpty() }
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, bitmap.height - 2)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        assertTrue("$name drew nothing", other > 100)
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun drawsANotebookInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").size(400.dp, 900.dp).background(PersonaTheme.palette.background)) {
                    NotebookFile(notebook, "/w", PaddingValues(), header = {}, onOpenPath = {}, loadImage = { null })
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            capture("notebook-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
    }
}
