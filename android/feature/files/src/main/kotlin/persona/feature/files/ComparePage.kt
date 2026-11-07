package persona.feature.files

import android.content.res.Resources
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.pdf.PdfRenderer
import android.os.Build
import android.os.ParcelFileDescriptor
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.drawscope.clipRect
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withStyle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import java.io.File
import java.security.MessageDigest

/** What comparing a document's text came to. */
internal sealed interface DocOutcome {
    data class Changes(val hunks: List<Hunk>, val added: Int, val removed: Int) : DocOutcome
    data object TooBig : DocOutcome
    data class Failed(val message: String) : DocOutcome
}

/** Two versions' units compared: their changed stretches, or too many changes to show line by line. */
internal fun compareUnits(before: List<DocUnit>, after: List<DocUnit>): DocOutcome {
    val ops = DocumentCompare.diff(DocumentCompare.flatten(before), DocumentCompare.flatten(after)) ?: return DocOutcome.TooBig
    val lines = ops.filterNot(DocumentCompare::isHeader)
    return DocOutcome.Changes(DocumentCompare.hunks(ops), lines.count { it.kind == OpKind.Add }, lines.count { it.kind == OpKind.Del })
}

/** A revision in words: HEAD, Index, Working tree, "before abc1234", a short hash. */
internal fun revLabel(res: Resources, rev: String): String = when {
    rev == "HEAD" -> rev
    rev == "INDEX" -> res.getString(R.string.files_compare_index)
    rev == WORKTREE -> res.getString(R.string.files_compare_worktree)
    rev.endsWith("^") -> res.getString(R.string.files_compare_before_commit, rev.dropLast(1).take(7))
    else -> rev.take(7)
}

private class ResourceNames(private val res: Resources) : UnitNames {
    override fun page(n: Int) = res.getString(R.string.files_compare_page, n)
    override fun slide(n: Int) = res.getString(R.string.files_compare_slide, n)
    override fun sheet(name: String) = res.getString(R.string.files_compare_sheet, name)
    override val notes: String get() = res.getString(R.string.files_compare_notes)
}

/**
 * A changed document's or picture's two versions (#2805; iOS `CompareView`, web `CompareView.tsx`): pictures side by
 * side or one over the other under a slider; documents as a diff of their text by page, slide or sheet. ⋯ opens either
 * version in the file viewer.
 */
@Composable
internal fun ComparePage(selection: FileSelection.Compare, state: FilesState, controller: FilesController, back: () -> Unit) {
    val res = LocalResources.current
    val context = LocalContext.current
    val sides = state.compare
    var menu by remember { mutableStateOf(false) }
    val name = FilesPaths.name(selection.path)
    JPage(name, subtitle = "${revLabel(res, selection.before)} → ${revLabel(res, selection.after)}", onBack = back,
        actions = { if (sides != null) JIconButton(PhIcons.DotsThree, stringResource(R.string.files_git_change_options), { menu = true }) }) { padding ->
        when {
            state.previewError != null -> LazyColumn(contentPadding = padding) {
                item { RetryState(stringResource(R.string.files_compare_versions_failed), state.previewError, controller::reload) }
            }
            sides == null -> LazyColumn(contentPadding = padding) { item { Loading() } }
            else -> key(sides) {
                val kind = DocumentCompare.kind(selection.path)
                val ext = FilesPaths.extension(selection.path).lowercase()
                // Each version's bytes into this module's cache (git's copy or the tree's file), read once per page.
                val local by produceState<Result<Pair<File?, File?>>?>(null, sides) {
                    value = runCatching {
                        val folder = File(context.cacheDir, "compare").apply { mkdirs() }
                        // Copies are named by version: ones not used for a day go (a version once seen stays a while).
                        withContext(Dispatchers.IO) {
                            folder.listFiles()?.filter { it.lastModified() < System.currentTimeMillis() - 86_400_000L }?.forEach { it.delete() }
                        }
                        suspend fun fetch(side: CompareSide): File? = side.path?.let { path ->
                            val file = File(folder, digest(side.rev + "\u0000" + path) + "." + ext)
                            // The tree's file changes under the same name; git's copy of a version does not.
                            if (side.rev == WORKTREE || !file.isFile) controller.download(path, file) else file.setLastModified(System.currentTimeMillis())
                            file
                        }
                        fetch(sides.before) to fetch(sides.after)
                    }.onFailure { if (it is CancellationException) throw it }
                }
                val files = local
                when {
                    files == null -> Column(Modifier.padding(padding)) { Loading() }
                    files.isFailure -> LazyColumn(contentPadding = padding) {
                        item { RetryState(stringResource(R.string.files_compare_versions_failed), files.exceptionOrNull()?.message.orEmpty(), controller::reload) }
                    }
                    kind == CompareKind.Image -> {
                        val pictures by produceState<Pair<Picture, Picture>?>(null, files) {
                            value = withContext(Dispatchers.IO) { files.getOrThrow().let { (a, b) -> picture(a) to picture(b) } }
                        }
                        val shown = pictures
                        if (shown == null) Column(Modifier.padding(padding)) { Loading() }
                        else ImageCompare(shown.first, shown.second, revLabel(res, sides.before.rev), revLabel(res, sides.after.rev), Modifier.padding(padding),
                            top = { Halves(selection, controller) })
                    }
                    else -> {
                        val outcome by produceState<DocOutcome?>(null, files) {
                            value = withContext(Dispatchers.Default) {
                                try {
                                    val (a, b) = files.getOrThrow()
                                    val names = ResourceNames(res)
                                    compareUnits(a?.let { units(ext, it, names) }.orEmpty(), b?.let { units(ext, it, names) }.orEmpty())
                                } catch (failure: CompareFailure) {
                                    DocOutcome.Failed(res.getString(failure.reason))
                                } catch (cancelled: CancellationException) { throw cancelled }
                                catch (error: Exception) { DocOutcome.Failed(error.message.orEmpty()) }
                                catch (_: OutOfMemoryError) { DocOutcome.Failed(res.getString(R.string.files_compare_too_big_file)) }
                            }
                        }
                        DocCompare(outcome, padding, top = { Halves(selection, controller) })
                    }
                }
            }
        }
    }
    if (menu && sides != null) JActionSheet(listOfNotNull(
        sides.before.path?.let { path -> JAction(stringResource(R.string.files_compare_open_old), PhIcons.FileText) { controller.push(FileSelection.File(path)) } },
        sides.after.path?.let { path -> JAction(stringResource(R.string.files_compare_open_new), PhIcons.FileText) { controller.push(FileSelection.File(path)) } },
    ), { menu = false }, name)
}

private fun digest(text: String): String =
    MessageDigest.getInstance("SHA-256").digest(text.toByteArray()).take(12).joinToString("") { "%02x".format(it) }

/** One side of a picture comparison: not in that version, there but the phone can't draw it, or the picture. */
internal sealed interface Picture {
    data object Missing : Picture
    data object Unreadable : Picture
    data class Shown(val bitmap: ImageBitmap) : Picture
}

/** A picture, scaled down on the way in so a large one doesn't fill the phone's memory. */
private fun picture(file: File?): Picture {
    if (file == null) return Picture.Missing
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    BitmapFactory.decodeFile(file.path, bounds)
    var sample = 1
    while (maxOf(bounds.outWidth, bounds.outHeight) / sample > 2048) sample *= 2
    return BitmapFactory.decodeFile(file.path, BitmapFactory.Options().apply { inSampleSize = sample })?.asImageBitmap()?.let(Picture::Shown)
        ?: Picture.Unreadable
}

/** A document read whole is kept to this size; past it the page says to open the versions instead. */
private const val MaxDocumentBytes = 64L shl 20

private fun units(ext: String, file: File, names: UnitNames): List<DocUnit> {
    if (file.length() > MaxDocumentBytes) throw CompareFailure(R.string.files_compare_too_big_file)
    return if (ext == "pdf") pdfUnits(file, names) else DocumentText.units(ext, file.readBytes(), names)
}

/** Unstaged / Staged for a change that has both halves (the text diff's switch). */
@Composable
private fun Halves(selection: FileSelection.Compare, controller: FilesController) {
    val change = selection.change
    if (!selection.bothHalves || change == null) return
    JSegmented(listOf(JSegment(stringResource(R.string.files_git_unstaged)), JSegment(stringResource(R.string.files_git_staged))),
        if (selection.staged) 1 else 0, { controller.preview(compareChange(change, staged = it == 1)) },
        Modifier.padding(horizontal = Tokens.Space.l, vertical = Tokens.Space.s))
}

/** A PDF's text page by page: the system reads it from Android 15 on; before that the page says so. */
private fun pdfUnits(file: File, names: UnitNames): List<DocUnit> {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.VANILLA_ICE_CREAM) throw CompareFailure(R.string.files_compare_pdf_unsupported)
    return ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY).use { descriptor ->
        PdfRenderer(descriptor).use { pdf ->
            (0 until minOf(pdf.pageCount, 500)).map { i ->
                pdf.openPage(i).use { page ->
                    DocUnit(names.page(i + 1), DocumentText.linesOf(page.textContents.joinToString("\n") { it.text }))
                }
            }
        }
    }
}

/** The pictures side by side, or (both there) the new one under the old cut at a slider. */
@Composable
internal fun ImageCompare(beforeSide: Picture, afterSide: Picture, beforeLabel: String, afterLabel: String, modifier: Modifier = Modifier,
    top: @Composable () -> Unit = {}) {
    val palette = PersonaTheme.palette
    var slide by rememberSaveable { mutableStateOf(false) }
    var at by rememberSaveable { mutableFloatStateOf(0.5f) }
    val before = (beforeSide as? Picture.Shown)?.bitmap
    val after = (afterSide as? Picture.Shown)?.bitmap
    val both = before != null && after != null
    Column(modifier.fillMaxSize().padding(Tokens.Space.l), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        top()
        if (both) JSegmented(listOf(JSegment(stringResource(R.string.files_compare_side_by_side)), JSegment(stringResource(R.string.files_compare_slider))),
            if (slide) 1 else 0, { slide = it == 1 }, Modifier.align(Alignment.CenterHorizontally))
        if (slide && both) {
            BoxWithConstraints(Modifier.weight(1f).fillMaxWidth().background(palette.sunken, PersonaTheme.shapes.controlShape).clipToBounds()) {
                Image(after, stringResource(R.string.files_compare_new), Modifier.fillMaxSize(), contentScale = ContentScale.Fit)
                Image(before, stringResource(R.string.files_compare_old), Modifier.fillMaxSize().drawWithContent {
                    clipRect(right = size.width * at) { this@drawWithContent.drawContent() }
                }, contentScale = ContentScale.Fit)
                // The divider where the old picture ends.
                Box(Modifier.fillMaxHeight().width(Tokens.Space.xxs).offset(x = maxWidth * at - Tokens.Space.xxs / 2).background(palette.accent))
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                Text(stringResource(R.string.files_compare_old), style = PersonaTheme.type.caption, color = palette.diffDel, maxLines = 1)
                JSlider(at, { at = it }, Modifier.weight(1f), contentDescription = stringResource(R.string.files_compare_divider))
                Text(stringResource(R.string.files_compare_new), style = PersonaTheme.type.caption, color = palette.diffAdd, maxLines = 1)
            }
        } else Row(Modifier.weight(1f), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            for ((i, side) in listOf(beforeSide, afterSide).withIndex()) Column(Modifier.weight(1f).fillMaxHeight(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                Text(stringResource(if (i == 0) R.string.files_compare_old else R.string.files_compare_new) + " · " + if (i == 0) beforeLabel else afterLabel,
                    style = PersonaTheme.type.caption, color = if (i == 0) palette.diffDel else palette.diffAdd, maxLines = 1)
                Box(Modifier.weight(1f).fillMaxWidth().background(palette.sunken, PersonaTheme.shapes.controlShape).padding(Tokens.Space.s),
                    contentAlignment = Alignment.Center) {
                    when (side) {
                        is Picture.Shown -> Image(side.bitmap, null, Modifier.fillMaxSize(), contentScale = ContentScale.Fit)
                        Picture.Missing -> Text(stringResource(R.string.files_compare_not_in_version), style = PersonaTheme.type.caption, color = palette.ink2)
                        Picture.Unreadable -> Text(stringResource(R.string.files_image_failed), style = PersonaTheme.type.caption, color = palette.ink2)
                    }
                }
            }
        }
    }
}

/** The text of two versions compared: each change under its page / slide / sheet with two lines around it. */
@Composable
internal fun DocCompare(outcome: DocOutcome?, padding: PaddingValues, top: @Composable () -> Unit = {}) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    when (outcome) {
        null -> Column(Modifier.padding(padding)) { Loading() }
        is DocOutcome.Failed -> LazyColumn(contentPadding = padding) {
            item { JEmptyState(stringResource(R.string.files_compare_failed), outcome.message) }
        }
        DocOutcome.TooBig -> LazyColumn(contentPadding = padding) {
            item { JEmptyState(stringResource(R.string.files_compare_too_big), stringResource(R.string.files_compare_open_both)) }
        }
        is DocOutcome.Changes -> LazyColumn(Modifier.fillMaxSize(), contentPadding = padding) {
            item("halves") { top() }
            item("summary") {
                Text(pluralStringResource(R.plurals.files_compare_summary, outcome.hunks.size, outcome.added, outcome.removed, outcome.hunks.size),
                    Modifier.padding(horizontal = Tokens.Space.l, vertical = Tokens.Space.s), style = type.caption, color = palette.ink2)
            }
            if (outcome.hunks.isEmpty()) item {
                JEmptyState(stringResource(R.string.files_compare_no_text_change), stringResource(R.string.files_compare_no_text_change_detail))
            }
            outcome.hunks.forEachIndexed { h, hunk ->
                if (hunk.skippedBefore > 0) item("skip$h") {
                    Text(pluralStringResource(R.plurals.files_compare_skipped, hunk.skippedBefore, hunk.skippedBefore), Modifier.padding(horizontal = Tokens.Space.l, vertical = Tokens.Space.xs),
                        style = type.caption, color = palette.ink3)
                }
                stickyHeader("unit$h") {
                    Text(hunk.unit, Modifier.fillMaxWidth().background(palette.background).padding(horizontal = Tokens.Space.l, vertical = Tokens.Space.xs),
                        style = type.label, color = palette.ink2, maxLines = 1)
                }
                items(DocumentCompare.pairRows(hunk.ops)) { line -> CompareRow(line) }
            }
        }
    }
}

@Composable
private fun CompareRow(line: CompareLine) {
    val palette = PersonaTheme.palette
    val (mark, text, tone) = when (line) {
        is CompareLine.Same -> Triple("", buildAnnotatedString { append(line.text) }, null)
        is CompareLine.Added -> Triple("+", buildAnnotatedString { append(line.text) }, palette.diffAdd)
        is CompareLine.Removed -> Triple("−", buildAnnotatedString { withStyle(SpanStyle(textDecoration = TextDecoration.LineThrough)) { append(line.text) } }, palette.diffDel)
        is CompareLine.Row -> Triple("~", buildAnnotatedString {
            line.cells.forEachIndexed { k, (old, new) ->
                if (k > 0) withStyle(SpanStyle(color = palette.ink3)) { append(DocumentCompare.ROW_SEP) }
                if (old == new) append(new) else {
                    withStyle(SpanStyle(color = palette.diffDel, textDecoration = TextDecoration.LineThrough)) { append(old) }
                    withStyle(SpanStyle(color = palette.ink3)) { append(" → ") }
                    withStyle(SpanStyle(color = palette.diffAdd)) { append(new) }
                }
            }
        }, palette.caution)
    }
    Row(Modifier.fillMaxWidth().background(palette.sunken)
        .padding(horizontal = Tokens.Space.l, vertical = Tokens.Space.xxs), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        Text(mark, Modifier.width(Tokens.Space.m), style = PersonaTheme.type.code, color = tone ?: palette.ink3, maxLines = 1)
        Text(text, Modifier.weight(1f), style = PersonaTheme.type.body, color = if (line is CompareLine.Same) palette.ink2 else palette.ink)
    }
}
