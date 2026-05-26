# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.5"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-macos-arm64.tar.gz"
      sha256 "5a44852d54ceb05a8525a37500bd53dbab7a0c990cdd6045d889c23a4b038fa0"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-macos-x86_64.tar.gz"
      sha256 "6d6d8355284d981c00249228d2c243b4a9af28b7785653b6de8ac7461ec08cdf"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-linux-arm64.tar.gz"
      sha256 "064ca76f8a4ee286bbb80e060f9a6e7a05a7be69f85c7afd1a53997a1d476351"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.5/gestaltd-linux-x86_64.tar.gz"
      sha256 "71fdd64f3c17f547f60126edb18226938d546bd247e4d178e5997509b5ce267d"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
