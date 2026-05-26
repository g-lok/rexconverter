class Rexconverter < Formula
  desc "Convert ReCycle (.rex/.rx2) files to cued WAV for M8 and DAWs"
  homepage "https://github.com/g-lok/rexconverter"
  license "MIT"

  on_macos do
    on_arm do
      version = "REPLACE_ME"
      url "https://github.com/g-lok/rexconverter/releases/download/v#{version}/rexconverter-#{version}-macos.tar.gz"
      sha256 "REPLACE_ME"
    end
    on_intel do
      version = "REPLACE_ME"
      url "https://github.com/g-lok/rexconverter/releases/download/v#{version}/rexconverter-#{version}-macos.tar.gz"
      sha256 "REPLACE_ME"
    end
  end

  def install
    bin.install "rexconverter"
    frameworks.install "Frameworks/REX Shared Library.framework"
  end

  def caveats
    <<~EOS
      rexconverter requires the REX Shared Library framework to be present
      in /Applications or alongside the binary.

      If you encounter "Library not loaded" errors, run:
        brew install --cask rex-shared-library
    EOS
  end

  test do
    system "#{bin}/rexconverter", "--version"
  end
end
