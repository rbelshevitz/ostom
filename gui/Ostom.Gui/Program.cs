using System.Diagnostics;
using System.Globalization;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Ostom.Gui;

internal static class Program
{
    [STAThread]
    private static void Main()
    {
        ApplicationConfiguration.Initialize();
        Application.Run(new MainForm());
    }
}

internal sealed class MainForm : Form
{
    private const long AutoAnalyzeLimit = 1L * 1024 * 1024 * 1024;
    private readonly bool en = !CultureInfo.CurrentUICulture.TwoLetterISOLanguageName.Equals("ru", StringComparison.OrdinalIgnoreCase);
    private readonly TextBox source = new();
    private readonly TextBox output = new();
    private readonly CheckBox eml = new();
    private readonly Button analyze = new();
    private readonly Button run = new();
    private readonly Button cancel = new();
    private readonly ProgressBar progress = new();
    private readonly RichTextBox result = new();
    private readonly Label statusText = new();
    private readonly Label progressText = new();
    private Process? child;
    private CancellationTokenSource? cancellation;
    private bool analysisReady;
    private string analyzedSource = "";
    private DateTime lastProgressPaintUtc;
    private string lastProgressText = "";

    private string T(string ru, string english) => en ? english : ru;

    public MainForm()
    {
        Text = T("OSTOM — восстановление писем из PST/OST", "OSTOM — recover mail from PST/OST");
        StartPosition = FormStartPosition.CenterScreen;
        ClientSize = new Size(820, 550);
        MinimumSize = new Size(700, 460);
        MaximizeBox = false;
        SizeGripStyle = SizeGripStyle.Show;
        Font = new Font("Segoe UI", 9F);
        BackColor = Color.FromArgb(236, 239, 242);
        ForeColor = Color.FromArgb(32, 39, 48);
        AutoScaleMode = AutoScaleMode.Dpi;
        Icon = Icon.ExtractAssociatedIcon(Application.ExecutablePath);
        BuildLayout();
    }

    private void BuildLayout()
    {
        const int actionButtonWidth = 128;
        var layout = new TableLayoutPanel { Dock = DockStyle.Fill, Padding = new Padding(12, 10, 12, 8), ColumnCount = 1, RowCount = 3, BackColor = BackColor };
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 174));
        layout.RowStyles.Add(new RowStyle(SizeType.Percent, 100));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 30));
        Controls.Add(layout);

        var inputGroup = new GroupBox { Text = T("Источник и экспорт", "Source and export"), Dock = DockStyle.Fill, Padding = new Padding(10, 8, 10, 8), ForeColor = Color.FromArgb(70, 78, 87) };
        var inputLayout = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 3, RowCount = 3, BackColor = BackColor };
        inputLayout.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 116));
        inputLayout.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        inputLayout.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, actionButtonWidth));
        inputLayout.RowStyles.Add(new RowStyle(SizeType.Percent, 40));
        inputLayout.RowStyles.Add(new RowStyle(SizeType.Percent, 40));
        inputLayout.RowStyles.Add(new RowStyle(SizeType.Absolute, 46));
        inputGroup.Controls.Add(inputLayout);
        inputLayout.Controls.Add(MakeFieldLabel(T("Исходный файл", "Source file")), 0, 0);
        source.Dock = DockStyle.Fill;
        source.ReadOnly = true;
        source.TabStop = false;
        source.Cursor = Cursors.Default;
        StyleInput(source);
        inputLayout.Controls.Add(source, 1, 0);
        var open = MakeActionButton(T("Открыть…", "Open…"));
        open.Click += async (_, _) => await PickSourceAsync();
        inputLayout.Controls.Add(open, 2, 0);
        inputLayout.Controls.Add(MakeFieldLabel(T("Папка экспорта", "Export folder")), 0, 1);
        output.Dock = DockStyle.Fill;
        output.ReadOnly = true;
        output.TabStop = false;
        output.Cursor = Cursors.Default;
        StyleInput(output);
        inputLayout.Controls.Add(output, 1, 1);
        var folder = MakeActionButton(T("Папка…", "Folder…"));
        folder.Click += (_, _) => PickFolder();
        inputLayout.Controls.Add(folder, 2, 1);

        var actions = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 5, RowCount = 1, Padding = new Padding(0, 2, 0, 0) };
        actions.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        actions.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, actionButtonWidth));
        actions.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, actionButtonWidth));
        actions.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 8));
        actions.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, actionButtonWidth));
        eml.Text = T("Дополнительно сохранить EML", "Save individual EML files too");
        eml.AutoSize = false;
        eml.Dock = DockStyle.Fill;
        eml.Margin = new Padding(0, 2, 0, 0);
        actions.Controls.Add(eml);
        ConfigureActionButton(analyze, T("Анализ", "Analyze"));
        SetActionButtonState(analyze, false);
        analyze.Click += async (_, _) => { if (!string.IsNullOrWhiteSpace(source.Text)) await AnalyzeAsync(source.Text); };
        actions.Controls.Add(analyze);
        ConfigureActionButton(run, T("Экспорт", "Export"));
        SetActionButtonState(run, false, true);
        run.Click += async (_, _) => await RunAsync();
        actions.Controls.Add(run);
        ConfigureActionButton(cancel, T("Отмена", "Cancel"));
        cancel.Enabled = false;
        cancel.Click += (_, _) => cancellation?.Cancel();
        actions.Controls.Add(cancel, 4, 0);
        inputLayout.Controls.Add(actions, 0, 2);
        inputLayout.SetColumnSpan(actions, 3);
        layout.Controls.Add(inputGroup, 0, 0);

        var reportGroup = new GroupBox { Text = T("Результат", "Result"), Dock = DockStyle.Fill, Padding = new Padding(10, 8, 10, 8), ForeColor = Color.FromArgb(70, 78, 87) };
        var reportLayout = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 1, RowCount = 3, BackColor = BackColor };
        reportLayout.RowStyles.Add(new RowStyle(SizeType.Absolute, 22));
        reportLayout.RowStyles.Add(new RowStyle(SizeType.Absolute, 18));
        reportLayout.RowStyles.Add(new RowStyle(SizeType.Percent, 100));
        reportGroup.Controls.Add(reportLayout);
        progressText.Text = T("Результат появится после анализа", "The analysis result will appear here");
        progressText.Dock = DockStyle.Fill;
        progressText.ForeColor = Color.FromArgb(91, 102, 115);
        progressText.TextAlign = ContentAlignment.MiddleLeft;
        reportLayout.Controls.Add(progressText, 0, 0);
        progress.Dock = DockStyle.Fill;
        progress.Margin = new Padding(0, 4, 0, 0);
        progress.MinimumSize = new Size(0, 12);
        progress.ForeColor = Color.FromArgb(37, 126, 166);
        progress.BackColor = Color.FromArgb(224, 231, 237);
        progress.Style = ProgressBarStyle.Marquee;
        progress.MarqueeAnimationSpeed = 28;
        progress.Visible = false;
        reportLayout.Controls.Add(progress, 0, 1);
        result.Dock = DockStyle.Fill;
        result.ReadOnly = true;
        result.BackColor = Color.White;
        result.BorderStyle = BorderStyle.FixedSingle;
        result.Font = new Font("Segoe UI", 9.5F);
        result.ForeColor = Color.Black;
        result.Padding = new Padding(10);
        result.ScrollBars = RichTextBoxScrollBars.Vertical;
        reportLayout.Controls.Add(result, 0, 2);
        layout.Controls.Add(reportGroup, 0, 1);
        statusText.Text = T("Ожидаем файл", "Waiting for file");
        statusText.Dock = DockStyle.Fill;
        statusText.TextAlign = ContentAlignment.MiddleLeft;
        statusText.Padding = new Padding(8, 0, 18, 0);
        statusText.Font = new Font("Segoe UI Semibold", 9F);
        statusText.BorderStyle = BorderStyle.FixedSingle;
        statusText.BackColor = Color.FromArgb(232, 242, 236);
        statusText.ForeColor = Color.FromArgb(55, 100, 76);
        layout.Controls.Add(statusText, 0, 2);
        FormClosing += (_, _) => { if (child is { HasExited: false }) child.Kill(entireProcessTree: true); };
    }

    private Label MakeFieldLabel(string text) => new() { Text = text, Dock = DockStyle.Fill, TextAlign = ContentAlignment.BottomLeft, ForeColor = Color.Black, Font = new Font("Segoe UI Semibold", 9F) };

    private static void StyleInput(TextBox box)
    {
        box.BorderStyle = BorderStyle.FixedSingle;
        box.BackColor = Color.White;
        box.Margin = new Padding(0, 4, 8, 2);
        box.Font = new Font("Segoe UI", 10F);
    }

    private static Button MakeActionButton(string text)
    {
        var button = new Button();
        ConfigureActionButton(button, text);
        return button;
    }

    private static void ConfigureActionButton(Button button, string text)
    {
        button.Text = text;
        button.AutoSize = false;
        button.Dock = DockStyle.None;
        button.Anchor = AnchorStyles.Top | AnchorStyles.Left | AnchorStyles.Right;
        button.Margin = new Padding(3, 1, 3, 1);
        button.MinimumSize = new Size(0, 32);
        button.Height = 32;
        button.Padding = new Padding(8, 0, 8, 0);
        button.FlatStyle = FlatStyle.Flat;
        button.FlatAppearance.BorderColor = Color.FromArgb(184, 196, 207);
        button.FlatAppearance.MouseOverBackColor = Color.FromArgb(226, 238, 244);
        button.BackColor = Color.White;
        button.ForeColor = Color.FromArgb(35, 67, 86);
        button.Font = new Font("Segoe UI Semibold", 9F);
    }

    private static void SetActionButtonState(Button button, bool enabled, bool primary = false)
    {
        button.Enabled = enabled;
        if (!enabled)
        {
            button.BackColor = Color.FromArgb(235, 239, 242);
            button.ForeColor = Color.FromArgb(155, 164, 172);
            button.FlatAppearance.BorderColor = Color.FromArgb(210, 218, 224);
            button.FlatAppearance.MouseOverBackColor = Color.FromArgb(235, 239, 242);
            return;
        }

        button.BackColor = primary ? Color.FromArgb(31, 102, 130) : Color.White;
        button.ForeColor = primary ? Color.White : Color.FromArgb(35, 67, 86);
        button.FlatAppearance.BorderColor = primary ? Color.FromArgb(31, 102, 130) : Color.FromArgb(184, 196, 207);
        button.FlatAppearance.MouseOverBackColor = primary ? Color.FromArgb(25, 82, 105) : Color.FromArgb(226, 238, 244);
    }

    private async Task PickSourceAsync()
    {
        using var dialog = new OpenFileDialog { Filter = T("Файлы Outlook (*.pst;*.ost)|*.pst;*.ost", "Outlook files (*.pst;*.ost)|*.pst;*.ost"), CheckFileExists = true, Multiselect = false };
        if (dialog.ShowDialog(this) != DialogResult.OK) return;
        var ext = Path.GetExtension(dialog.FileName).ToLowerInvariant();
        if (ext is not ".pst" and not ".ost") return;
        source.Text = dialog.FileName;
        output.Text = dialog.FileName + ".ostom-export";
        analysisReady = false;
        analyzedSource = "";
        SetActionButtonState(run, false, true);
        var size = new FileInfo(dialog.FileName).Length;
        if (size <= AutoAnalyzeLimit)
            await AnalyzeAsync(dialog.FileName);
        else
        {
            SetActionButtonState(analyze, true);
            result.Text = T($"Файл большой ({FormatBytes(size)}). Нажмите «Анализ», когда будете готовы проверить содержимое.", $"Large file ({FormatBytes(size)}). Click Analyze when you are ready to inspect its contents.");
        }
    }

    private void PickFolder()
    {
        using var dialog = new FolderBrowserDialog { Description = T("Выберите папку для экспорта mbox", "Select the mbox export folder"), UseDescriptionForTitle = true, ShowNewFolderButton = true };
        if (dialog.ShowDialog(this) == DialogResult.OK)
        {
            output.Text = dialog.SelectedPath;
        }
    }

    private async Task RunAsync()
    {
        if (string.IsNullOrWhiteSpace(source.Text) || string.IsNullOrWhiteSpace(output.Text))
        {
            result.Text = T("Укажите исходный файл и папку экспорта.", "Select a source file and an export folder.");
            return;
        }
        if (!analysisReady || !string.Equals(analyzedSource, Path.GetFullPath(source.Text), StringComparison.OrdinalIgnoreCase))
        {
            result.Text = T("Сначала выполните анализ файла.", "Analyze the file before exporting.");
            return;
        }
        var cli = Path.Combine(AppContext.BaseDirectory, "ostom.exe");
        if (!File.Exists(cli))
        {
            result.Text = T("Рядом с GUI не найден ostom.exe.", "ostom.exe was not found alongside the GUI.");
            return;
        }
        SetBusy(true);
        cancellation = new CancellationTokenSource();
        try
        {
            var args = new List<string> { "--json", "--progress-json", "--output-dir", output.Text, "--export-dir", output.Text };
            if (eml.Checked) args.Add("--eml");
            args.Add(source.Text);
            ShowFriendly(T("Экспорт выполняется…", "Export is running…"));
            var report = await RunCliAsync(cli, args, cancellation.Token);
            ShowReport(report);
        }
        catch (OperationCanceledException)
        {
            if (child is { HasExited: false }) child.Kill(entireProcessTree: true);
            result.Text = T("Операция отменена.", "Operation cancelled.");
        }
        finally
        {
            SetBusy(false);
            cancellation?.Dispose();
            cancellation = null;
            child?.Dispose();
            child = null;
        }
    }

    private async Task AnalyzeAsync(string file)
    {
        if (child is { HasExited: false }) return;
        analysisReady = false;
        analyzedSource = "";
        SetActionButtonState(run, false, true);
        var cli = Path.Combine(AppContext.BaseDirectory, "ostom.exe");
        if (!File.Exists(cli)) { ShowFriendly(T("Рядом с GUI не найден ostom.exe.", "ostom.exe was not found alongside the GUI.")); return; }
        SetBusy(true);
        cancellation = new CancellationTokenSource();
        try
        {
            ShowFriendly(T("Анализ файла…", "Analyzing file…"));
            var args = new List<string> { "--json", "--analyze", "--progress-json" };
            if (!string.IsNullOrWhiteSpace(output.Text)) args.AddRange(new[] { "--output-dir", output.Text });
            args.Add(file);
            var report = await RunCliAsync(cli, args, cancellation.Token);
            ShowReport(report);
            var parsed = JsonSerializer.Deserialize<GuiReport>(report);
            if (parsed is not null && parsed.Status != "failed")
            {
                analysisReady = true;
                analyzedSource = Path.GetFullPath(file);
            }
        }
        catch (OperationCanceledException) { ShowFriendly(T("Анализ отменён.", "Analysis cancelled.")); }
        catch (Exception ex) { ShowFriendly(T("Не удалось выполнить анализ: " + ex.Message, "Analysis failed: " + ex.Message)); }
        finally
        {
            SetBusy(false);
            cancellation?.Dispose();
            cancellation = null;
            child?.Dispose();
            child = null;
        }
    }

    private async Task<string> RunCliAsync(string cli, IEnumerable<string> args, CancellationToken token)
    {
        var psi = new ProcessStartInfo(cli) { UseShellExecute = false, CreateNoWindow = true, RedirectStandardOutput = true, RedirectStandardError = true, WorkingDirectory = AppContext.BaseDirectory };
        foreach (var arg in args) psi.ArgumentList.Add(arg);
        child = new Process { StartInfo = psi };
        child.Start();
        var stdout = child.StandardOutput.ReadToEndAsync(token);
        var progressOutput = DrainProgressAsync(child.StandardError, token);
        var wait = child.WaitForExitAsync(token);
        await Task.WhenAll(stdout, progressOutput, wait);
        return await stdout;
    }

    private async Task DrainProgressAsync(StreamReader reader, CancellationToken token)
    {
        while (await reader.ReadLineAsync(token) is { } line) HandleProgress(line);
    }

    private void HandleProgress(string line)
    {
        try
        {
            using var doc = JsonDocument.Parse(line);
            var root = doc.RootElement;
            if (!root.TryGetProperty("event", out var kind) || kind.GetString() != "progress") return;
            var label = root.GetProperty("label").GetString() ?? "";
            var done = root.GetProperty("done").GetInt32();
            var total = root.GetProperty("total").GetInt32();
            var etaKnown = root.TryGetProperty("eta_known", out var etaFlag) && etaFlag.GetBoolean();
            var etaSeconds = root.TryGetProperty("eta_seconds", out var etaValue) ? etaValue.GetInt32() : 0;
            var rate = root.TryGetProperty("rate", out var rateValue) ? rateValue.GetDouble() : 0;
            var unit = root.TryGetProperty("unit", out var unitValue) ? unitValue.GetString() ?? "items" : "items";
            var count = total > 0
                ? $"{T("Обработано", "Processed")}: {done} {T("из", "of")} {total}"
                : $"{T("Обработано", "Processed")}: {done}";
            var suffix = etaKnown
                ? $" · {T("осталось", "about")} {FormatDuration(etaSeconds)}"
                : rate > 0
                    ? $" · {rate:0.0} {FormatRateUnit(unit)}"
                    : "";
            var displayText = count + suffix;
            var now = DateTime.UtcNow;
            if ((now - lastProgressPaintUtc).TotalMilliseconds < 250) return;
            lastProgressPaintUtc = now;
            if (!IsDisposed) BeginInvoke(() =>
            {
                if (total > 0)
                {
                    progress.Visible = true;
                    progress.Style = ProgressBarStyle.Continuous;
                    progress.Maximum = Math.Max(1, total);
                    progress.Value = Math.Min(Math.Max(0, done), progress.Maximum);
                }
                else
                {
                    progress.Visible = true;
                    progress.Style = ProgressBarStyle.Marquee;
                }
                if (lastProgressText == displayText) return;
                lastProgressText = displayText;
                progressText.Text = displayText;
            });
        }
        catch (JsonException) { }
    }

    private string FormatDuration(int seconds)
    {
        if (seconds < 60) return $"{seconds} {T("с", "s")}";
        var span = TimeSpan.FromSeconds(seconds);
        return span.TotalHours >= 1
            ? $"{(int)span.TotalHours}:{span.Minutes:00}:{span.Seconds:00}"
            : $"{span.Minutes}:{span.Seconds:00}";
    }

    private string FormatRateUnit(string unit) => unit switch
    {
        "messages" => T("сообщений/с", "messages/s"),
        "candidates" => T("сообщений/с", "messages/s"),
        "folders" => T("папок/с", "folders/s"),
        "bytes" => T("байт/с", "bytes/s"),
        _ => T("элементов/с", "items/s")
    };

    private string FriendlyProgress(string value) => value switch
    {
        "preparing export" => T("Подготовка оценки", "Preparing estimate"),
        "scanning recoverable messages" => T("Обнаружено сообщений", "Messages found"),
        "writing recovered messages" => T("Восстановлено сообщений", "Messages recovered"),
        "standard mailbox export" => T("Обработано сообщений", "Messages processed"),
        "analyzing recoverable messages" => T("Проверено сообщений", "Messages checked"),
        _ => value
    };

    private void ShowReport(string json)
    {
        try
        {
            var report = JsonSerializer.Deserialize<GuiReport>(json);
            if (report is null) throw new InvalidDataException();
            var indexedTimeout = report.Warnings.Any(w => w.StartsWith("indexed export: indexed worker exceeded", StringComparison.OrdinalIgnoreCase));
            var fallbackUsed = report.NativeRecovery?.Export is { Written: > 0 };
            var text = new StringBuilder();
            text.AppendLine($"{T("Состояние", "Status")}: {FriendlyStatus(report.Status, report.CheckOnly, indexedTimeout && !fallbackUsed, report.Analysis is not null)}");
            text.AppendLine($"{T("Структура", "Structure")}: {FriendlyFinding(report.Findings.Structure, report.CheckOnly)}");
            var messagesFinding = FriendlyFinding(report.Findings.Messages, report.CheckOnly);
            var attachmentsFinding = FriendlyFinding(report.Findings.Attachments, report.CheckOnly);
            if (indexedTimeout && report.NativeRecovery?.Export is not null)
            {
                messagesFinding = fallbackUsed
                    ? T($"восстановлено {report.NativeRecovery.Export.Written}", $"{report.NativeRecovery.Export.Written} recovered")
                    : T("не удалось завершить чтение файла", "file reading could not be completed");
                attachmentsFinding = T($"восстановлено {report.NativeRecovery.Export.Attachments}", $"{report.NativeRecovery.Export.Attachments} recovered");
            }
            text.AppendLine($"{T("Сообщения", "Messages")}: {messagesFinding}");
            text.AppendLine($"{T("Вложения", "Attachments")}: {attachmentsFinding}");
            if (report.Analysis is not null)
                text.AppendLine($"{T("Рекомендация", "Recommendation")}: {FriendlyOutlook(report.Assessment.RecoveryOutlook, report.Findings.Structure, true, fallbackUsed)}");
            if (report.Analysis is not null)
            {
                text.AppendLine($"{T("Найдено сообщений", "Messages found")}: {report.Analysis.Candidates}");
                text.AppendLine($"{T("Найдено вложений", "Attachments found for recovery")}: {report.Analysis.Attachments}");
                if (report.Analysis.IncompleteMessages > 0) text.AppendLine($"{T("Неполных сообщений", "Incomplete messages")}: {report.Analysis.IncompleteMessages}");
                if (report.Analysis.IncompleteAttachments > 0) text.AppendLine($"{T("Неполных вложений", "Incomplete attachments")}: {report.Analysis.IncompleteAttachments}");
                if (report.Analysis.Rejected > 0) text.AppendLine($"{T("Пропущено кандидатов", "Rejected candidates")}: {report.Analysis.Rejected}");
                if (report.Analysis.LimitReached) text.AppendLine(T("Анализ достиг безопасного лимита; экспорт можно продолжить отдельно.", "The safe analysis limit was reached; export can continue separately."));
            }
            else if (report.CheckOnly) text.AppendLine(T("Это быстрая проверка структуры; содержимое писем и вложений будет проверено во время экспорта.", "This is a fast structural check; message and attachment content will be checked during export."));
            if (report.Storage is not null && report.Analysis is not null && report.Analysis.Candidates > 0 && report.Storage.RequiredEstimate > 0) text.AppendLine($"{T("Свободно на диске", "Free disk space")}: {FormatBytes(report.Storage.AvailableBytes)}; {T("оценка экспорта", "export estimate")}: {FormatBytes(report.Storage.RequiredEstimate)}");
            else if (!fallbackUsed && report.IndexedExport is not null) text.AppendLine($"{T("Экспортировано", "Exported")}: {report.IndexedExport.Messages} {T("сообщений", "messages")}, {report.IndexedExport.Attachments} {T("вложений", "attachments")}");
            if (!fallbackUsed && report.NativeRecovery?.Export is not null) text.AppendLine($"{T("Восстановлено", "Recovered")}: {report.NativeRecovery.Export.Written} {T("сообщений", "messages")}, {report.NativeRecovery.Export.Attachments} {T("вложений", "attachments")}");
            foreach (var warning in report.Warnings.Where(w => !w.Equals("check-only mode: export was not requested", StringComparison.OrdinalIgnoreCase) && !w.StartsWith("indexed export: indexed worker exceeded", StringComparison.OrdinalIgnoreCase))) text.AppendLine($"{T("Предупреждение", "Warning")}: {warning}");
            foreach (var error in report.Errors) text.AppendLine($"{T("Ошибка", "Error")}: {error}");
            result.Text = text.ToString();
            progressText.Text = report.Analysis is not null
                ? T("Анализ завершён", "Analysis complete")
                : T("Операция завершена", "Operation complete");
            var reportStatus = FriendlyStatus(report.Status, report.CheckOnly, indexedTimeout && !fallbackUsed, report.Analysis is not null);
            if (report.Errors.Count > 0)
                SetStatus(reportStatus, Color.FromArgb(251, 235, 235), Color.FromArgb(165, 65, 62));
            else if (report.Warnings.Count > 0 || indexedTimeout)
                SetStatus(reportStatus, Color.FromArgb(255, 245, 224), Color.FromArgb(145, 91, 28));
            else
                SetStatus(reportStatus, Color.FromArgb(232, 242, 236), Color.FromArgb(55, 100, 76));
        }
        catch { result.Text = T("Не удалось прочитать итоговый отчёт CLI.", "Could not read the CLI final report."); }
    }

    private void ShowFriendly(string text)
    {
        result.Text = text;
        SetStatus(text, Color.FromArgb(255, 245, 224), Color.FromArgb(145, 91, 28));
    }

    private void SetStatus(string text, Color background, Color foreground)
    {
        statusText.Text = text;
        statusText.BackColor = background;
        statusText.ForeColor = foreground;
    }

    private string FriendlyStatus(string value, bool checkOnly, bool indexedTimeout, bool contentAnalysis = false) => indexedTimeout ? T("Завершено частично", "Completed with limitations") : value switch
    {
        "completed" when checkOnly || contentAnalysis => T("Анализ завершён", "Analysis complete"),
        "completed" => T("Готово", "Complete"),
        "completed with errors" => T("Завершено с предупреждениями", "Completed with warnings"),
        "failed" => T("Не удалось выполнить", "Failed"),
        _ => value
    };

    private string FriendlyFinding(string value, bool checkOnly) => value switch
    {
        "not_checked" when checkOnly => T("будут проверены при экспорте", "will be checked during export"),
        "not_checked" => T("ещё не проверялись", "not checked yet"),
        "healthy" => T("прочиталась нормально", "read successfully"),
        "complete" => T("найдены и прочитаны", "available and readable"),
        "degraded" => T("есть повреждения", "damaged or incomplete"),
        "partial" => T("частично восстановлены", "partially recovered"),
        "timeout" => T("обычный проход остановлен защитным лимитом", "regular pass stopped by safety limit"),
        "failed" => T("не удалось прочитать", "could not be read"),
        _ => value
    };

    private string FriendlyOutlook(string value, string structure, bool contentAnalysis, bool fallbackUsed) => fallbackUsed
        ? T("восстановление выполнено; результат сохранён", "recovery completed; result saved")
        : structure == "healthy"
        ? T("восстановление не требуется; можно экспортировать", "no recovery is needed; ready to export")
        : value switch
    {
        "good" => T("хорошая", "good"),
        "uncertain" => T("нужна проверка повреждённых участков", "some damaged areas need checking"),
        "limited" => T("часть данных может быть недоступна; восстановим всё, что удастся прочитать", "some data may be unavailable; OSTOM will recover everything it can read"),
        _ => value
    };

    private static string FormatBytes(long value) => FormatBytes((ulong)Math.Max(0, value));
    private static string FormatBytes(ulong value)
    {
        var units = new[] { "B", "KiB", "MiB", "GiB", "TiB" };
        double n = value; var i = 0;
        while (n >= 1024 && i < units.Length - 1) { n /= 1024; i++; }
        return $"{n:0.0} {units[i]}";
    }

    private void SetBusy(bool busy)
    {
        if (busy)
        {
            lastProgressPaintUtc = DateTime.MinValue;
            lastProgressText = "";
        }
        SetActionButtonState(run, !busy && analysisReady, true);
        SetActionButtonState(analyze, !busy && !string.IsNullOrWhiteSpace(source.Text));
        eml.Enabled = !busy;
        cancel.Enabled = busy;
        progress.Visible = busy;
        if (busy) progressText.Text = T("Выполняется операция…", "Operation in progress…");
        if (busy)
        {
            SetStatus(T("Выполняется", "Working"), Color.FromArgb(255, 245, 224), Color.FromArgb(145, 91, 28));
        }
        UseWaitCursor = busy;
    }
}

internal sealed class GuiReport
{
    [JsonPropertyName("status")] public string Status { get; set; } = "";
    [JsonPropertyName("check_only")] public bool CheckOnly { get; set; }
    [JsonPropertyName("findings")] public GuiFindings Findings { get; set; } = new();
    [JsonPropertyName("assessment")] public GuiAssessment Assessment { get; set; } = new();
    [JsonPropertyName("storage")] public GuiStorage? Storage { get; set; }
    [JsonPropertyName("indexed_export")] public GuiIndexed? IndexedExport { get; set; }
    [JsonPropertyName("native_recovery")] public GuiNative? NativeRecovery { get; set; }
    [JsonPropertyName("analysis")] public GuiAnalysis? Analysis { get; set; }
    [JsonPropertyName("warnings")] public List<string> Warnings { get; set; } = new();
    [JsonPropertyName("errors")] public List<string> Errors { get; set; } = new();
}
internal sealed class GuiFindings { [JsonPropertyName("structure")] public string Structure { get; set; } = ""; [JsonPropertyName("messages")] public string Messages { get; set; } = ""; [JsonPropertyName("attachments")] public string Attachments { get; set; } = ""; }
internal sealed class GuiAssessment { [JsonPropertyName("recovery_outlook")] public string RecoveryOutlook { get; set; } = ""; }
internal sealed class GuiStorage { [JsonPropertyName("available_bytes")] public ulong AvailableBytes { get; set; } [JsonPropertyName("required_estimate_bytes")] public ulong RequiredEstimate { get; set; } }
internal sealed class GuiIndexed { [JsonPropertyName("messages")] public int Messages { get; set; } [JsonPropertyName("attachments")] public int Attachments { get; set; } }
internal sealed class GuiNative { [JsonPropertyName("export")] public GuiNativeExport? Export { get; set; } }
internal sealed class GuiNativeExport { [JsonPropertyName("written")] public int Written { get; set; } [JsonPropertyName("attachments")] public int Attachments { get; set; } }
internal sealed class GuiAnalysis
{
    [JsonPropertyName("candidates")] public int Candidates { get; set; }
    [JsonPropertyName("attachments")] public int Attachments { get; set; }
    [JsonPropertyName("incomplete_messages")] public int IncompleteMessages { get; set; }
    [JsonPropertyName("incomplete_attachments")] public int IncompleteAttachments { get; set; }
    [JsonPropertyName("rejected")] public int Rejected { get; set; }
    [JsonPropertyName("limit_reached")] public bool LimitReached { get; set; }
}
