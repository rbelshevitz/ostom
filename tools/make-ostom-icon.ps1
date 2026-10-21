Add-Type -AssemblyName System.Drawing

$sourcePath = Join-Path $PSScriptRoot '..\gui\Ostom.Gui\ostom-icon-generated.png'
$targetPath = Join-Path $PSScriptRoot '..\gui\Ostom.Gui\ostom.ico'
$source = [Drawing.Image]::FromFile($sourcePath)
$bitmap = [Drawing.Bitmap]::new(256, 256, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
$graphics = [Drawing.Graphics]::FromImage($bitmap)
$graphics.Clear([Drawing.Color]::Transparent)
$graphics.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
$graphics.SmoothingMode = [Drawing.Drawing2D.SmoothingMode]::HighQuality
$graphics.DrawImage($source, 0, 0, 256, 256)

$sizes = @(16, 32, 48, 64, 128, 256)
$images = @()
foreach ($size in $sizes) {
    $small = [Drawing.Bitmap]::new($size, $size, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $smallGraphics = [Drawing.Graphics]::FromImage($small)
    $smallGraphics.Clear([Drawing.Color]::Transparent)
    $smallGraphics.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $smallGraphics.SmoothingMode = [Drawing.Drawing2D.SmoothingMode]::HighQuality
    $smallGraphics.DrawImage($source, 0, 0, $size, $size)
    $smallStream = [IO.MemoryStream]::new()
    $small.Save($smallStream, [Drawing.Imaging.ImageFormat]::Png)
    $images += [PSCustomObject]@{ Size = $size; Data = $smallStream.ToArray() }
    $smallGraphics.Dispose(); $small.Dispose(); $smallStream.Dispose()
}

$icoStream = [IO.File]::Create($targetPath)
$writer = [IO.BinaryWriter]::new($icoStream)
$writer.Write([UInt16]0)
$writer.Write([UInt16]1)
$writer.Write([UInt16]$images.Count)
$offset = 6 + (16 * $images.Count)
foreach ($image in $images) {
    $dimension = if ($image.Size -eq 256) { 0 } else { $image.Size }
    $writer.Write([Byte]$dimension)
    $writer.Write([Byte]$dimension)
    $writer.Write([Byte]0)
    $writer.Write([Byte]0)
    $writer.Write([UInt16]1)
    $writer.Write([UInt16]32)
    $writer.Write([UInt32]$image.Data.Length)
    $writer.Write([UInt32]$offset)
    $offset += $image.Data.Length
}
foreach ($image in $images) { $writer.Write($image.Data) }
$writer.Flush()

$writer.Dispose(); $icoStream.Dispose()
$graphics.Dispose(); $bitmap.Dispose(); $source.Dispose()
